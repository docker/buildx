package dap

import (
	"context"
	"path/filepath"
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
	def  *llb.Definition
	ops  map[digest.Digest]*pb.Op
	head digest.Digest
	bps  map[digest.Digest]int

	frames         map[int32]*frame
	framesByDigest map[digest.Digest]*frame

	// Runtime state for the evaluate call.
	entrypoint *step

	// Controls pause.
	paused chan stepType
	mu     sync.Mutex

	// Attributes set when a thread is paused.
	cancel     context.CancelCauseFunc // invoked when the thread is resumed
	rCtx       *build.ResultHandle
	curPos     digest.Digest
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
		ref  gateway.Reference
		next = t.entrypoint
		err  error
	)
	for next != nil {
		event := t.needsDebug(next, action, err)
		if event.Reason != "" {
			select {
			case action = <-t.pause(ctx, ref, err, next, event):
				// do nothing here
			case <-ctx.Done():
				return context.Cause(ctx)
			}
		}

		if err != nil {
			return err
		}

		if action == stepContinue {
			t.setBreakpoints(ctx)
		}
		ref, next, err = t.seekNext(ctx, next, action)
	}
	return nil
}

func (t *thread) init(ctx Context, c gateway.Client, ref gateway.Reference, meta map[string][]byte, inputs build.Inputs) error {
	t.c = c
	t.ref = ref
	t.meta = meta
	t.sourcePath = inputs.ContextPath

	if err := t.getLLBState(ctx); err != nil {
		return err
	}
	return t.createProgram()
}

type step struct {
	// dgst holds the digest that should be resolved by this step.
	// If this is empty, no digest should be resolved.
	dgst digest.Digest

	// in holds the next target when step in is used.
	in *step

	// out holds the next target when step out is used.
	out *step

	// next holds the next target when next is used.
	next *step

	// frame will hold the stack frame associated with this step.
	frame *frame
}

func (t *thread) createProgram() error {
	t.framesByDigest = make(map[digest.Digest]*frame)
	t.frames = make(map[int32]*frame)

	// Create the entrypoint by using the last node.
	// We will build on top of that.
	head := &step{
		dgst:  t.head,
		frame: t.getStackFrame(t.head),
	}
	t.entrypoint = t.createBranch(head)
	return nil
}

func (t *thread) createBranch(last *step) (first *step) {
	first = last
	for first.dgst != "" {
		prev := &step{
			// set to first temporarily until we determine
			// if there are other inputs.
			in: first,
			// always first
			next: first,
			// exit point always matches the one set on first
			out: first.out,
			// always set to the same as next which is always first
			frame: t.getStackFrame(first.dgst),
		}

		op := t.ops[first.dgst]
		if len(op.Inputs) > 0 {
			parent := t.determineParent(op)
			for i := len(op.Inputs) - 1; i >= 0; i-- {
				if i == parent {
					// Skip the direct parent.
					continue
				}
				inp := op.Inputs[i]

				// Create a pseudo-step that acts as an exit point for this
				// branch. This step exists so this branch has a place to go
				// after it has finished that will advance to the next
				// instruction.
				exit := &step{
					in:    prev.in,
					next:  prev.next,
					out:   prev.out,
					frame: prev.frame,
				}

				head := &step{
					dgst:  digest.Digest(inp.Digest),
					in:    exit,
					next:  exit,
					out:   exit,
					frame: t.getStackFrame(digest.Digest(inp.Digest)),
				}
				prev.in = t.createBranch(head)
			}

			// Set the digest of the parent input on the first step associated
			// with this step if it exists.
			if parent >= 0 {
				prev.dgst = digest.Digest(op.Inputs[parent].Digest)
			}
		}

		// New first is the step we just created.
		first = prev
	}
	return first
}

func (t *thread) getStackFrame(dgst digest.Digest) *frame {
	if f := t.framesByDigest[dgst]; f != nil {
		return f
	}

	f := &frame{
		op: t.ops[dgst],
	}
	f.Id = int(t.idPool.Get())
	if meta, ok := t.def.Metadata[dgst]; ok {
		f.setNameFromMeta(meta)
	}
	if loc, ok := t.def.Source.Locations[string(dgst)]; ok {
		f.fillLocation(t.def, loc, t.sourcePath)
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
		} else if next := cur.in; next != nil {
			if id, ok := t.bps[next.dgst]; ok {
				e.Reason = "breakpoint"
				e.Description = "Paused on breakpoint"
				e.HitBreakpointIds = []int{id}
			}
		}
	}
	return
}

func (t *thread) pause(c Context, ref gateway.Reference, err error, pos *step, event dap.StoppedEventBody) <-chan stepType {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.paused != nil {
		return t.paused
	}

	t.paused = make(chan stepType, 1)
	if err != nil {
		var solveErr *errdefs.SolveError
		if errors.As(err, &solveErr) {
			if dt, err := solveErr.Op.MarshalVT(); err == nil {
				t.curPos = digest.FromBytes(dt)
			}
		}
	}

	ctx, cancel := context.WithCancelCause(c)
	t.collectStackTrace(ctx, pos, ref)
	t.cancel = cancel

	if ref != nil || err != nil {
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
		if err := op.UnmarshalVT(dt); err != nil {
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

func (t *thread) seekNext(ctx Context, from *step, action stepType) (gateway.Reference, *step, error) {
	// If we're at the end, return no digest to signal that
	// we should conclude debugging.
	var target *step
	switch action {
	case stepNext:
		target = from.next
	case stepIn:
		target = from.in
	case stepOut:
		target = from.out
	case stepContinue:
		target = t.continueDigest(from)
	}
	return t.seek(ctx, target)
}

func (t *thread) seek(ctx Context, target *step) (ref gateway.Reference, result *step, err error) {
	if target != nil {
		if target.dgst != "" {
			ref, err = t.solve(ctx, target.dgst)
			if err != nil {
				return ref, nil, err
			}
		}

		result = target
	} else {
		ref = t.ref
	}

	if ref != nil {
		if err = ref.Evaluate(ctx); err != nil {
			// If this is not a solve error, do not return the
			// reference and target step.
			var solveErr *errdefs.SolveError
			if errors.As(err, &solveErr) {
				if dt, err := solveErr.Op.MarshalVT(); err == nil {
					// Find the error digest.
					errDgst := digest.FromBytes(dt)

					// Iterate from the first step to find the one
					// we failed on.
					result = t.entrypoint
					for result != nil {
						next := result.in
						if next != nil && next.dgst == errDgst {
							break
						}
						result = next
					}
				}
			} else {
				return nil, nil, err
			}
		}
	}
	return ref, result, err
}

func (t *thread) continueDigest(from *step) *step {
	if len(t.bps) == 0 {
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
		for cur != nil {
			next := cur.in
			if next != nil && isBreakpoint(next.dgst) {
				return cur
			}
			cur = next
		}
		return nil
	}
	return next(from)
}

func (t *thread) solve(ctx context.Context, target digest.Digest) (gateway.Reference, error) {
	if target == t.head {
		return t.ref, nil
	}

	head := &pb.Op{
		Inputs: []*pb.Input{{Digest: string(target)}},
	}
	dt, err := head.MarshalVT()
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

func (t *thread) collectStackTrace(ctx context.Context, pos *step, ref gateway.Reference) {
	for pos != nil {
		frame := pos.frame
		frame.ExportVars(ctx, ref, t.variables)
		t.stackTrace = append(t.stackTrace, int32(frame.Id))
		pos, ref = pos.out, nil
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
