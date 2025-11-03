package dap

import (
	"context"
	"path"
	"path/filepath"
	"slices"
	"sync"

	"github.com/docker/buildx/build"
	"github.com/docker/buildx/dap/common"
	"github.com/google/go-dap"
	"github.com/moby/buildkit/client/llb"
	gateway "github.com/moby/buildkit/frontend/gateway/client"
	"github.com/moby/buildkit/solver/errdefs"
	"github.com/moby/buildkit/solver/pb"
	"github.com/opencontainers/go-digest"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
)

type thread struct {
	// Persistent data.
	id   int
	name string

	// Persistent state from the adapter.
	sharedState
	variables *variableReferences

	// Inputs to the evaluate call.
	c          gateway.Client
	ref        gateway.Reference
	meta       map[string][]byte
	sourcePath string

	// LLB state for the evaluate call.
	def    *llb.Definition
	ops    map[digest.Digest]*pb.Op
	head   digest.Digest
	bps    map[digest.Digest]int
	frames map[int32]*frame

	// Runtime state for the evaluate call.
	entrypoint *step

	// Controls pause.
	paused chan stepType
	mu     sync.Mutex

	// Attributes set when a thread is paused.
	cancel     context.CancelCauseFunc // invoked when the thread is resumed
	rCtx       *build.ResultHandle
	stackTrace []int32
}

type stepType int

const (
	stepContinue stepType = iota
	stepNext
	stepIn
	stepOut
)

func (t *thread) Evaluate(ctx Context, c gateway.Client, headRef gateway.Reference, meta map[string][]byte, inputs build.Inputs, cfg common.Config) error {
	if err := t.init(ctx, c, headRef, meta, inputs); err != nil {
		return err
	}
	defer t.reset()

	action := stepContinue
	if cfg.StopOnEntry {
		action = stepNext
	}

	var (
		k    string
		refs map[string]gateway.Reference
		next = t.entrypoint
		err  error
	)
	for next != nil {
		event := t.needsDebug(next, action, err)
		if event.Reason != "" {
			select {
			case action = <-t.pause(ctx, k, refs, err, next, event):
				// do nothing here
			case <-ctx.Done():
				return context.Cause(ctx)
			}
		}

		if err != nil {
			return err
		}

		t.setBreakpoints(ctx)
		k, next, refs, err = t.seekNext(ctx, next, action)
	}
	return nil
}

func (t *thread) init(ctx Context, c gateway.Client, ref gateway.Reference, meta map[string][]byte, inputs build.Inputs) error {
	t.c = c
	t.ref = ref
	t.meta = meta

	// Combine the dockerfile directory with the context path to find the
	// real base path. The frontend will report the base path as the filename.
	dir := path.Dir(inputs.DockerfilePath)
	if !path.IsAbs(dir) {
		dir = path.Join(inputs.ContextPath, dir)
	}
	t.sourcePath = dir

	if err := t.getLLBState(ctx); err != nil {
		return err
	}
	return t.createProgram()
}

type step struct {
	// dgst holds the digest associated with this step. This is used for
	// breakpoint resolution.
	dgst digest.Digest

	// in holds the next target when step in is used.
	in *step

	// out holds the next target when step out is used.
	out *step

	// next holds the next target when next is used.
	next *step

	// frame will hold the stack frame associated with this step.
	frame *frame

	// parent holds the index of the parent step.
	parent int
}

func (t *thread) createProgram() error {
	t.frames = make(map[int32]*frame)

	// Create the entrypoint by using the last node.
	// We will build on top of that.
	t.entrypoint = t.createBranch(t.head, nil)
	return nil
}

func (t *thread) createBranch(dgst digest.Digest, exitpoint *step) (entrypoint *step) {
	// Construct the final two steps in this branch. The final steps
	// both point to the same line. The difference between them is one
	// step is before the execution of the digest and the other is
	// after the execution of that digest.
	returnpoint := &step{
		in:     exitpoint,
		next:   exitpoint,
		out:    exitpoint,
		parent: -1,
	}

	entrypoint = &step{
		dgst:   dgst,
		in:     returnpoint,
		next:   returnpoint,
		out:    exitpoint,
		frame:  t.getStackFrame(dgst, nil),
		parent: -1,
	}

	// Create a pseudo-frame and attach it to the return point.
	// This is mostly used for getting the correct inputs utilized
	// by this frame.
	//
	// We don't save this frame or assign it a unique ID as it should
	// never be returned.
	returnpoint.frame = &frame{
		StackFrame: entrypoint.frame.StackFrame,
		op: &pb.Op{
			Inputs: []*pb.Input{
				{Digest: string(dgst), Index: 0},
			},
		},
	}

	for {
		// Construct the input step for this digest based on the inputs.
		op := t.ops[entrypoint.dgst]
		if len(op.Inputs) == 0 {
			return entrypoint
		}

		entrypoint.parent = t.determineParent(op)
		for i := len(op.Inputs) - 1; i >= 0; i-- {
			if i == entrypoint.parent {
				// Skip the direct parent.
				continue
			}

			// When we find inputs that aren't the direct parent,
			// we want to add them as a step before the current step.
			// We have to do a few things when inserting this.
			//
			// 1. We move the digest from the old entrypoint to this node.
			// 		This is so the breakpoint happens before these inputs
			// 		are evaluated.
			// 2. We keep the next/out pointers the same but redirect in
			// 		to point to the new branch.
			// 3. The direct parent is excluded from this logic. We handle
			// 		that later.
			inp := op.Inputs[i]

			head := *entrypoint
			entrypoint.dgst = ""

			// Create the routine associated with this input.
			// Associate it with the entrypoint in step.
			head.in = t.createBranch(digest.Digest(inp.Digest), entrypoint)
			entrypoint = &head
		}

		// If we have no direct parent, return the current entrypoint
		// as the beginning.
		if entrypoint.parent < 0 {
			return entrypoint
		}

		// Create a new step that refers to the direct parent.
		head := &step{
			dgst:   digest.Digest(op.Inputs[entrypoint.parent].Digest),
			in:     entrypoint,
			next:   entrypoint,
			out:    entrypoint.out,
			parent: -1,
		}
		head.frame = t.getStackFrame(head.dgst, entrypoint)
		entrypoint = head
	}
}

func (t *thread) getStackFrame(dgst digest.Digest, next *step) *frame {
	f := &frame{
		op: t.ops[dgst],
	}
	f.Id = int(t.idPool.Get())
	if meta, ok := t.def.Metadata[dgst]; ok {
		f.setNameFromMeta(meta)
	}
	if loc, ok := t.def.Source.Locations[string(dgst)]; ok {
		f.fillLocation(t.def, loc, t.sourcePath, next)
	}
	t.frames[int32(f.Id)] = f
	return f
}

func (t *thread) determineParent(op *pb.Op) int {
	// Another section should have already checked this but
	// double check here just in case we forget somewhere else.
	// The rest of this method assumes there's at least one parent
	// at index zero.
	n := len(op.Inputs)
	if n == 0 {
		return -1
	}

	switch op := op.Op.(type) {
	case *pb.Op_Exec:
		for _, m := range op.Exec.Mounts {
			if m.Dest == "/" {
				return int(m.Input)
			}
		}
		return -1
	case *pb.Op_File:
		// Use the first input where the index is from one of the inputs.
		for _, action := range op.File.Actions {
			if input := int(action.Input); input >= 0 && input < n {
				return input
			}
		}

		// Default to having no parent.
		return -1
	default:
		// Default to index zero.
		return 0
	}
}

func (t *thread) reset() {
	t.c = nil
	t.ref = nil
	t.meta = nil
	t.sourcePath = ""
	t.ops = nil
}

func (t *thread) needsDebug(cur *step, step stepType, err error) (e dap.StoppedEventBody) {
	if err != nil {
		e.Reason = "exception"
		e.Description = "Encountered an error during result evaluation"
	} else if cur != nil {
		if step != stepContinue {
			e.Reason = "step"
		} else if id, ok := t.bps[cur.dgst]; ok {
			e.Reason = "breakpoint"
			e.Description = "Paused on breakpoint"
			e.HitBreakpointIds = []int{id}
		}
	}
	return
}

func (t *thread) pause(c Context, k string, refs map[string]gateway.Reference, err error, pos *step, event dap.StoppedEventBody) <-chan stepType {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.paused != nil {
		return t.paused
	}
	t.paused = make(chan stepType, 1)

	ctx, cancel := context.WithCancelCause(c)
	t.collectStackTrace(ctx, pos, refs)
	t.cancel = cancel

	// Used for exec. Only works if there was an error or if the step returns
	// a root mount.
	if ref, ok := refs[k]; ok || err != nil {
		t.prepareResultHandle(c, ref, err)
	}

	event.ThreadId = t.id
	c.C() <- &dap.StoppedEvent{
		Event: dap.Event{Event: "stopped"},
		Body:  event,
	}
	return t.paused
}

func (t *thread) prepareResultHandle(c Context, ref gateway.Reference, err error) {
	// Create a context for cancellations and make the cancel function
	// block on the wait group.
	var wg sync.WaitGroup
	ctx, cancel := context.WithCancelCause(c)
	t.cancel = func(cause error) {
		defer wg.Wait()
		cancel(cause)
	}

	t.rCtx = build.NewResultHandle(ctx, t.c, ref, t.meta, err)

	// Start the attach. Use the context we created and perform it in
	// a goroutine. We aren't necessarily assuming this will actually work.
	wg.Add(1)
	go func() {
		defer wg.Done()
		t.sh.Attach(ctx, t)
	}()
}

func (t *thread) Continue() {
	t.resume(stepContinue)
}

func (t *thread) Next() {
	t.resume(stepNext)
}

func (t *thread) StepIn() {
	t.resume(stepIn)
}

func (t *thread) StepOut() {
	t.resume(stepOut)
}

func (t *thread) resume(step stepType) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.paused == nil {
		return
	}
	t.releaseState()

	t.paused <- step
	close(t.paused)
	t.paused = nil
}

func (t *thread) StackTrace() []dap.StackFrame {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.paused == nil {
		// Cannot compute stack trace when not paused.
		// This should never happen, but protect ourself in
		// case it does.
		return []dap.StackFrame{}
	}

	frames := make([]dap.StackFrame, len(t.stackTrace))
	for i, id := range t.stackTrace {
		frames[i] = t.frames[id].StackFrame
	}
	return frames
}

func (t *thread) Scopes(frameID int) []dap.Scope {
	t.mu.Lock()
	defer t.mu.Unlock()

	frame := t.frames[int32(frameID)]
	return frame.Scopes()
}

func (t *thread) Variables(id int) []dap.Variable {
	return t.variables.Get(id)
}

func (t *thread) getLLBState(ctx Context) error {
	st, err := t.ref.ToState()
	if err != nil {
		return err
	}

	t.def, err = st.Marshal(ctx)
	if err != nil {
		return err
	}

	for _, src := range t.def.Source.Infos {
		fname := filepath.Join(t.sourcePath, src.Filename)
		t.sourceMap.Put(ctx, fname, src.Data)
	}

	t.ops = make(map[digest.Digest]*pb.Op, len(t.def.Def))
	for _, dt := range t.def.Def {
		dgst := digest.FromBytes(dt)

		var op pb.Op
		if err := op.Unmarshal(dt); err != nil {
			return err
		}
		t.ops[dgst] = &op
	}

	t.head, err = t.def.Head()
	return err
}

func (t *thread) setBreakpoints(ctx Context) {
	t.bps = t.breakpointMap.Intersect(ctx, t.def.Source, t.sourcePath)
}

func (t *thread) seekNext(ctx Context, from *step, action stepType) (string, *step, map[string]gateway.Reference, error) {
	// If we're at the end, return no digest to signal that
	// we should conclude debugging.
	var target *step
	switch action {
	case stepNext:
		target = t.continueDigest(from, from.next)
	case stepIn:
		target = from.in
	case stepOut:
		target = t.continueDigest(from, from.out)
	case stepContinue:
		target = t.continueDigest(from, nil)
	}
	return t.seek(ctx, target)
}

func (t *thread) seek(ctx Context, target *step) (k string, result *step, mounts map[string]gateway.Reference, err error) {
	k = "/"

	var refs map[string]gateway.Reference
	if target != nil {
		k, refs, err = t.solveInputs(ctx, target)
		if err != nil {
			return "", nil, nil, err
		}
		result = target
	} else {
		refs = map[string]gateway.Reference{"/": t.ref}
	}

	if len(refs) > 0 {
		if err := t.evaluateRefs(ctx, refs); err != nil {
			return t.rewind(ctx, err)
		}
	}
	return k, result, refs, nil
}

func (t *thread) continueDigest(from, until *step) *step {
	if len(t.bps) == 0 && until == nil {
		return nil
	}

	isBreakpoint := func(dgst digest.Digest) bool {
		if dgst == "" {
			return false
		}

		_, ok := t.bps[dgst]
		return ok
	}

	next := func(s *step) *step {
		cur := s.in
		for cur != nil && cur != until {
			if isBreakpoint(cur.dgst) {
				return cur
			}
			cur = cur.in
		}
		return until
	}
	return next(from)
}

func (t *thread) solveInputs(ctx context.Context, target *step) (string, map[string]gateway.Reference, error) {
	if target == nil || target.frame.op == nil {
		return "", nil, nil
	}
	op := target.frame.op

	var root string
	refs := make(map[string]gateway.Reference)
	for i, input := range op.Inputs {
		k := t.determineInputName(op, input)
		if _, ok := refs[k]; ok || k == "" {
			continue
		}

		if i == target.parent {
			root = k
		}

		ref, err := t.solve(ctx, input)
		if err != nil {
			return "", nil, err
		}
		refs[k] = ref
	}
	return root, refs, nil
}

func (t *thread) determineInputName(op *pb.Op, input *pb.Input) string {
	switch op := op.Op.(type) {
	case *pb.Op_Exec:
		for _, m := range op.Exec.Mounts {
			if m.Input >= 0 && m.Input == input.Index {
				return m.Dest
			}
		}
	}
	return input.Digest
}

func (t *thread) evaluateRefs(ctx context.Context, refs map[string]gateway.Reference) error {
	eg, _ := errgroup.WithContext(ctx)
	for _, ref := range refs {
		eg.Go(func() error {
			return ref.Evaluate(ctx)
		})
	}
	return eg.Wait()
}

func (t *thread) solve(ctx context.Context, input *pb.Input) (gateway.Reference, error) {
	if input.Digest == string(t.head) {
		return t.ref, nil
	}

	head := &pb.Op{
		Inputs: []*pb.Input{input},
	}
	dt, err := head.Marshal()
	if err != nil {
		return nil, err
	}

	def := t.def.ToPB()
	def.Def[len(def.Def)-1] = dt

	res, err := t.c.Solve(ctx, gateway.SolveRequest{
		Definition: def,
	})
	if err != nil {
		return nil, err
	}
	return res.SingleRef()
}

func (t *thread) releaseState() {
	if t.rCtx != nil {
		t.rCtx.Done()
		t.rCtx = nil
	}

	for _, f := range t.frames {
		f.ResetVars()
	}

	if t.cancel != nil {
		t.cancel(context.Canceled)
		t.cancel = nil
	}

	t.stackTrace = t.stackTrace[:0]
	t.variables.Reset()
}

func (t *thread) collectStackTrace(ctx context.Context, pos *step, mounts map[string]gateway.Reference) {
	for pos != nil {
		frame := pos.frame
		frame.ExportVars(ctx, mounts, t.variables)
		t.stackTrace = append(t.stackTrace, int32(frame.Id))
		pos, mounts = pos.out, nil
	}
}

func (t *thread) hasFrame(id int) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.paused == nil {
		return false
	}

	_, ok := t.frames[int32(id)]
	return ok
}

func (t *thread) rewind(ctx Context, inErr error) (k string, result *step, mounts map[string]gateway.Reference, retErr error) {
	var solveErr *errdefs.SolveError
	if !errors.As(inErr, &solveErr) {
		// If this is not a solve error, do not return the
		// reference and target step.
		return "", nil, nil, inErr
	}

	// Find the error digests we might have failed on.
	var digests []digest.Digest
	if dt, err := solveErr.Op.Marshal(); err == nil {
		digests = append(digests, digest.FromBytes(dt))
	}

	// Include a version of the digest without the platform
	// if this is a file op.
	if _, ok := solveErr.Op.Op.(*pb.Op_File); ok && solveErr.Op.Platform != nil {
		op := solveErr.Op.CloneVT()
		op.Platform = nil

		if dt, err := op.Marshal(); err == nil {
			digests = append(digests, digest.FromBytes(dt))
		}
	}

	if len(digests) == 0 {
		return "", nil, nil, inErr
	}

	// Iterate from the first step to find the one we failed on.
	result = t.entrypoint
	for result != nil && !slices.Contains(digests, result.dgst) {
		result = result.in
	}

	// Seek to this step. This should succeed because otherwise
	// we wouldn't have been able to even fail on it to begin with.
	k, result, mounts, retErr = t.seek(ctx, result)
	if retErr != nil {
		return k, result, mounts, retErr
	}
	return k, result, mounts, inErr
}
