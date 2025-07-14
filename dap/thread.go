package dap

import (
	"context"
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
)

type thread struct {
	// Persistent data.
	id   int
	name string

	// Persistent state from the adapter.
	idPool        *idPool
	sourceMap     *sourceMap
	breakpointMap *breakpointMap
	variables     *variableReferences

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

	// Runtime state for the evaluate call.
	regions         []*region
	regionsByDigest map[digest.Digest]int

	// Controls pause.
	paused chan stepType
	mu     sync.Mutex

	// Attributes set when a thread is paused.
	rCtx       *build.ResultHandle
	curPos     digest.Digest
	stackTrace []int32
	frames     map[int32]*frame
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

func (t *thread) Evaluate(ctx Context, c gateway.Client, ref gateway.Reference, meta map[string][]byte, inputs build.Inputs, cfg common.Config) error {
	if err := t.init(ctx, c, ref, meta, inputs); err != nil {
		return err
	}
	defer t.reset()

	step := stepContinue
	if cfg.StopOnEntry {
		step = stepNext
	}

	for {
		if step == stepContinue {
			t.setBreakpoints(ctx)
		}
		pos, err := t.seekNext(ctx, step)

		event := t.needsDebug(pos, step, err)
		if event.Reason == "" {
			return err
		}

		select {
		case step = <-t.pause(ctx, err, event):
			if err != nil {
				return err
			}
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

	if err := t.getLLBState(ctx); err != nil {
		return err
	}
	return t.createRegions()
}

func (t *thread) reset() {
	t.c = nil
	t.ref = nil
	t.meta = nil
	t.sourcePath = ""
	t.ops = nil
}

func (t *thread) needsDebug(target digest.Digest, step stepType, err error) (e dap.StoppedEventBody) {
	if err != nil {
		e.Reason = "exception"
		e.Description = "Encountered an error during result evaluation"
	} else if step == stepNext && target != "" {
		e.Reason = "step"
	} else if step == stepContinue {
		if id, ok := t.bps[target]; ok {
			e.Reason = "breakpoint"
			e.Description = "Paused on breakpoint"
			e.HitBreakpointIds = []int{id}
		}
	}
	return
}

func (t *thread) pause(c Context, err error, event dap.StoppedEventBody) <-chan stepType {
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
	t.collectStackTrace()

	event.ThreadId = t.id
	c.C() <- &dap.StoppedEvent{
		Event: dap.Event{Event: "stopped"},
		Body:  event,
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

func (t *thread) createRegions() error {
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
	switch step {
	case stepNext:
		target = t.nextDigest(nil)
	case stepContinue:
		target = t.continueDigest()
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

func (t *thread) nextDigest(fn func(digest.Digest) bool) digest.Digest {
	isValid := func(dgst digest.Digest) bool {
		// Skip this digest because it has no locations in the source file.
		if loc, ok := t.def.Source.Locations[string(dgst)]; !ok || len(loc.Locations) == 0 {
			return false
		}

		// If a custom function has been set for validation, use it.
		return fn == nil || fn(dgst)
	}

	// If we have no position, automatically select the first step.
	if t.curPos == "" {
		r := t.regions[len(t.regions)-1]
		if isValid(r.digests[0]) {
			return r.digests[0]
		}

		// We cannot use the first position. Treat the first position as our
		// current position so we can iterate.
		t.curPos = r.digests[0]
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
		if !isValid(next) {
			i++
			continue
		}
		return next
	}
}

func (t *thread) continueDigest() digest.Digest {
	if len(t.bps) == 0 {
		return t.head
	}

	isValid := func(dgst digest.Digest) bool {
		_, ok := t.bps[dgst]
		return ok
	}
	return t.nextDigest(isValid)
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
	t.stackTrace = nil
	t.frames = nil
}

func (t *thread) collectStackTrace() {
	region := t.regionsByDigest[t.curPos]
	r := t.regions[region]

	digests := r.digests
	if index := slices.Index(digests, t.curPos); index >= 0 {
		digests = digests[:index+1]
	}

	t.frames = make(map[int32]*frame)
	for i := len(digests) - 1; i >= 0; i-- {
		dgst := digests[i]

		frame := &frame{}
		frame.Id = int(t.idPool.Get())

		if meta, ok := t.def.Metadata[dgst]; ok {
			frame.setNameFromMeta(meta)
		}
		if loc, ok := t.def.Source.Locations[string(dgst)]; ok {
			frame.fillLocation(t.def, loc, t.sourcePath)
		}

		if op := t.ops[dgst]; op != nil {
			frame.fillVarsFromOp(op, t.variables)
		}
		t.stackTrace = append(t.stackTrace, int32(frame.Id))
		t.frames[int32(frame.Id)] = frame
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

func pop[S ~[]E, E any](s *S) E {
	e := (*s)[len(*s)-1]
	*s = (*s)[:len(*s)-1]
	return e
}
