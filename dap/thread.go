package dap

import (
	"context"
	"path/filepath"
	"slices"
	"sync"

	"github.com/docker/buildx/build"
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
	idPool    *idPool
	sourceMap *sourceMap

	// Inputs to the evaluate call.
	c          gateway.Client
	ref        gateway.Reference
	meta       map[string][]byte
	sourcePath string

	// LLB state for the evaluate call.
	def  *llb.Definition
	ops  map[digest.Digest]*pb.Op
	head digest.Digest

	// Runtime state for the evaluate call.
	regions         []*region
	regionsByDigest map[digest.Digest]int

	// Controls pause.
	paused chan stepType
	mu     sync.Mutex

	// Attributes set when a thread is paused.
	rCtx   *build.ResultHandle
	curPos digest.Digest

	// Lazy attributes that are set when a thread is paused.
	stackTrace []dap.StackFrame
}

type region struct {
	// dependsOn means this thread depends on the result of another thread.
	dependsOn map[int]struct{}

	// digests is a set of digests associated with this thread.
	digests []digest.Digest
}

type stepType int

const (
	stepContinue stepType = iota
	stepNext
)

func (t *thread) Evaluate(ctx Context, c gateway.Client, ref gateway.Reference, meta map[string][]byte, inputs build.Inputs) error {
	if err := t.init(ctx, c, ref, meta, inputs); err != nil {
		return err
	}
	defer t.reset()

	step := stepNext
	for {
		pos, err := t.seekNext(ctx, step)

		reason, desc := t.needsDebug(pos, step, err)
		if reason == "" {
			return err
		}

		select {
		case step = <-t.pause(ctx, err, reason, desc):
		case <-ctx.Done():
			return context.Cause(ctx)
		}
	}
}

func (t *thread) init(ctx Context, c gateway.Client, ref gateway.Reference, meta map[string][]byte, inputs build.Inputs) error {
	t.c = c
	t.ref = ref
	t.meta = meta
	t.sourcePath = inputs.ContextPath
	return t.createRegions(ctx)
}

func (t *thread) reset() {
	t.c = nil
	t.ref = nil
	t.meta = nil
	t.sourcePath = ""
	t.ops = nil
}

func (t *thread) needsDebug(target digest.Digest, step stepType, err error) (reason, desc string) {
	if err != nil {
		reason = "exception"
		desc = "Encountered an error during result evaluation"
	} else if target != "" && step == stepNext {
		reason = "step"
	}
	return
}

func (t *thread) pause(c Context, err error, reason, desc string) <-chan stepType {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.paused != nil {
		return t.paused
	}

	t.paused = make(chan stepType, 1)
	t.rCtx = build.NewResultHandle(c, t.c, t.ref, t.meta, err)
	if err != nil {
		var solveErr *errdefs.SolveError
		if errors.As(err, &solveErr) {
			if dt, err := solveErr.Op.MarshalVT(); err == nil {
				t.curPos = digest.FromBytes(dt)
			}
		}
	}

	c.C() <- &dap.StoppedEvent{
		Event: dap.Event{Event: "stopped"},
		Body: dap.StoppedEventBody{
			Reason:      reason,
			Description: desc,
			ThreadId:    t.id,
		},
	}
	return t.paused
}

func (t *thread) Continue() {
	t.resume(stepContinue)
}

func (t *thread) Next() {
	t.resume(stepNext)
}

func (t *thread) resume(step stepType) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.paused == nil {
		return
	}

	if t.rCtx != nil {
		t.rCtx.Done()
		t.rCtx = nil
	}

	if t.stackTrace != nil {
		for _, frame := range t.stackTrace {
			t.idPool.Put(int64(frame.Id))
		}
		t.stackTrace = nil
	}

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

	if t.stackTrace == nil {
		t.stackTrace = t.makeStackTrace()
	}
	return t.stackTrace
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

func (t *thread) findBacklinks() map[digest.Digest]map[digest.Digest]struct{} {
	backlinks := make(map[digest.Digest]map[digest.Digest]struct{})
	for dgst := range t.ops {
		backlinks[dgst] = make(map[digest.Digest]struct{})
	}

	for dgst, op := range t.ops {
		for _, inp := range op.Inputs {
			if digest.Digest(inp.Digest) == t.head {
				continue
			}
			backlinks[digest.Digest(inp.Digest)][dgst] = struct{}{}
		}
	}
	return backlinks
}

func (t *thread) createRegions(ctx Context) error {
	if err := t.getLLBState(ctx); err != nil {
		return err
	}

	// Find the links going from inputs to their outputs.
	// This isn't represented in the LLB graph but we need it to ensure
	// an op only has one child and whether we are allowed to visit a node.
	backlinks := t.findBacklinks()

	// Create distinct regions whenever we have any branch (inputs or outputs).
	t.regions = []*region{}
	t.regionsByDigest = map[digest.Digest]int{}

	determineRegion := func(dgst digest.Digest, children map[digest.Digest]struct{}) {
		if len(children) == 1 {
			var cDgst digest.Digest
			for d := range children {
				cDgst = d
			}
			childOp := t.ops[cDgst]

			if len(childOp.Inputs) == 1 {
				// We have one child and our child has one input so we can be merged
				// into the same region as our child.
				region := t.regionsByDigest[cDgst]
				t.regions[region].digests = append(t.regions[region].digests, dgst)
				t.regionsByDigest[dgst] = region
				return
			}
		}

		// We will require a new region for this digest because
		// we weren't able to merge it in within the existing regions.
		next := len(t.regions)
		t.regions = append(t.regions, &region{
			digests:   []digest.Digest{dgst},
			dependsOn: make(map[int]struct{}),
		})
		t.regionsByDigest[dgst] = next

		// Mark each child as depending on this new region.
		for child := range children {
			region := t.regionsByDigest[child]
			t.regions[region].dependsOn[next] = struct{}{}
		}
	}

	canVisit := func(dgst digest.Digest) bool {
		for dgst := range backlinks[dgst] {
			if _, ok := t.regionsByDigest[dgst]; !ok {
				// One of our outputs has not been categorized.
				return false
			}
		}
		return true
	}

	unvisited := []digest.Digest{t.head}
	for len(unvisited) > 0 {
		dgst := pop(&unvisited)
		op := t.ops[dgst]

		children := backlinks[dgst]
		determineRegion(dgst, children)

		// Determine which inputs we can now visit.
		for _, inp := range op.Inputs {
			indgst := digest.Digest(inp.Digest)
			if canVisit(indgst) {
				unvisited = append(unvisited, indgst)
			}
		}
	}

	// Reverse each of the digests so dependencies are first.
	// It is currently in reverse topological order and it needs to be in
	// topological order.
	for _, r := range t.regions {
		slices.Reverse(r.digests)
	}
	t.propagateRegionDependencies()
	return nil
}

// propagateRegionDependencies will propagate the dependsOn attribute between
// different regions to make dependency lookups easier. If A depends on B
// and B depends on C, then A depends on C. But the algorithm before this will only
// record direct dependencies.
func (t *thread) propagateRegionDependencies() {
	for _, r := range t.regions {
		for {
			n := len(r.dependsOn)
			for i := range r.dependsOn {
				for j := range t.regions[i].dependsOn {
					r.dependsOn[j] = struct{}{}
				}
			}

			if n == len(r.dependsOn) {
				break
			}
		}
	}
}

func (t *thread) seekNext(ctx Context, step stepType) (digest.Digest, error) {
	// If we're at the end, return no digest to signal that
	// we should conclude debugging.
	if t.curPos == t.head {
		return "", nil
	}

	target := t.head
	if step == stepNext {
		target = t.nextDigest()
	}

	if target == "" {
		return "", nil
	}
	return t.seek(ctx, target)
}

func (t *thread) seek(ctx Context, target digest.Digest) (digest.Digest, error) {
	ref, err := t.solve(ctx, target)
	if err != nil {
		return "", err
	}

	if err = ref.Evaluate(ctx); err != nil {
		var solveErr *errdefs.SolveError
		if errors.As(err, &solveErr) {
			if dt, err := solveErr.Op.MarshalVT(); err == nil {
				t.curPos = digest.FromBytes(dt)
			}
		} else {
			t.curPos = ""
		}
	} else {
		t.curPos = target
	}
	return t.curPos, err
}

func (t *thread) nextDigest() digest.Digest {
	// If we have no position, automatically select the first step.
	if t.curPos == "" {
		r := t.regions[len(t.regions)-1]
		return r.digests[0]
	}

	// Look up the region associated with our current position.
	// If we can't find it, just pretend we're using step continue.
	region, ok := t.regionsByDigest[t.curPos]
	if !ok {
		return t.head
	}

	r := t.regions[region]
	i := slices.Index(r.digests, t.curPos) + 1

	for {
		if i >= len(r.digests) {
			if region <= 0 {
				// We're at the end of our execution. Should have been caught by
				// t.head == t.curPos.
				return ""
			}
			region--

			r = t.regions[region]
			i = 0
			continue
		}

		next := r.digests[i]
		if loc, ok := t.def.Source.Locations[string(next)]; !ok || len(loc.Locations) == 0 {
			// Skip this digest because it has no locations in the source file.
			i++
			continue
		}
		return next
	}
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

func (t *thread) newStackFrame() dap.StackFrame {
	return dap.StackFrame{
		Id: int(t.idPool.Get()),
	}
}

func (t *thread) makeStackTrace() []dap.StackFrame {
	var frames []dap.StackFrame

	region := t.regionsByDigest[t.curPos]
	r := t.regions[region]

	digests := r.digests
	if index := slices.Index(digests, t.curPos); index >= 0 {
		digests = digests[:index+1]
	}

	for i := len(digests) - 1; i >= 0; i-- {
		dgst := digests[i]

		frame := t.newStackFrame()
		if meta, ok := t.def.Metadata[dgst]; ok {
			fillStackFrameMetadata(&frame, meta)
		}
		if loc, ok := t.def.Source.Locations[string(dgst)]; ok {
			t.fillStackFrameLocation(&frame, loc)
		}
		frames = append(frames, frame)
	}
	return frames
}

func fillStackFrameMetadata(frame *dap.StackFrame, meta llb.OpMetadata) {
	if name, ok := meta.Description["llb.customname"]; ok {
		frame.Name = name
	} else if cmd, ok := meta.Description["com.docker.dockerfile.v1.command"]; ok {
		frame.Name = cmd
	}
	// TODO: should we infer the name from somewhere else?
}

func (t *thread) fillStackFrameLocation(frame *dap.StackFrame, loc *pb.Locations) {
	for _, l := range loc.Locations {
		for _, r := range l.Ranges {
			frame.Line = int(r.Start.Line)
			frame.Column = int(r.Start.Character)
			frame.EndLine = int(r.End.Line)
			frame.EndColumn = int(r.End.Character)

			info := t.def.Source.Infos[l.SourceIndex]
			frame.Source = &dap.Source{
				Path: filepath.Join(t.sourcePath, info.Filename),
			}
			return
		}
	}
}

func pop[S ~[]E, E any](s *S) E {
	e := (*s)[len(*s)-1]
	*s = (*s)[:len(*s)-1]
	return e
}
