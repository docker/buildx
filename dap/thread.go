package dap

import (
	"context"
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
	id     int
	name   string
	idPool *idPool

	// Set during the evaluate call.
	c    gateway.Client
	ref  gateway.Reference
	meta map[string][]byte

	// Lazy state set during the evaluate call.
	// Some debug pathways don't require this information
	// while some might.
	def  *llb.Definition
	ops  map[digest.Digest]*pb.Op
	head digest.Digest

	// Controls pause.
	paused chan struct{}
	mu     sync.Mutex

	// Attributes set when a thread is paused.
	rCtx   *build.ResultHandle
	curPos digest.Digest

	// Lazy attributes that are set when a thread is paused.
	stackTrace []dap.StackFrame
}

func (t *thread) Evaluate(ctx Context, c gateway.Client, ref gateway.Reference, meta map[string][]byte, cfg build.InvokeConfig) error {
	if err := t.init(ctx, c, ref, meta); err != nil {
		return err
	}
	defer t.reset()

	err := t.ref.Evaluate(ctx)
	if reason, desc := t.needsDebug(cfg, err); reason != "" {
		select {
		case <-t.pause(ctx, err, reason, desc):
		case <-ctx.Done():
			t.Resume(ctx)
			return context.Cause(ctx)
		}
	}
	return err
}

func (t *thread) init(ctx context.Context, c gateway.Client, ref gateway.Reference, meta map[string][]byte) error {
	t.c = c
	t.ref = ref
	t.meta = meta
	return nil
}

func (t *thread) reset() {
	t.c = nil
	t.ref = nil
	t.meta = nil
	t.ops = nil
}

func (t *thread) needsDebug(cfg build.InvokeConfig, err error) (reason, desc string) {
	if !cfg.NeedsDebug(err) {
		return
	}

	if err != nil {
		reason = "exception"
		desc = "Encountered an error during result evaluation"
	} else {
		reason = "pause"
		desc = "Result evaluation completed"
	}
	return
}

func (t *thread) pause(c Context, err error, reason, desc string) <-chan struct{} {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.paused != nil {
		return t.paused
	}

	t.paused = make(chan struct{})
	t.rCtx = build.NewResultHandle(c, t.c, t.ref, t.meta, err)
	t.curPos = t.head
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

func (t *thread) Resume(c Context) {
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
		t.stackTrace = t.makeStackTrace(context.TODO())
	}
	return t.stackTrace
}

func (t *thread) requireLLBState(ctx context.Context) error {
	st, err := t.ref.ToState()
	if err != nil {
		return err
	}

	t.def, err = st.Marshal(ctx)
	if err != nil {
		return err
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

func (t *thread) newStackFrame() dap.StackFrame {
	return dap.StackFrame{
		Id: int(t.idPool.Get()),
	}
}

func (t *thread) makeStackTrace(ctx context.Context) []dap.StackFrame {
	if err := t.requireLLBState(ctx); err != nil {
		return []dap.StackFrame{}
	}

	var frames []dap.StackFrame
	for cur := t.curPos; ; {
		frame := t.newStackFrame()
		if meta, ok := t.def.Metadata[cur]; ok {
			fillStackFrameMetadata(&frame, meta)
		}
		if loc, ok := t.def.Source.Locations[string(cur)]; ok {
			fillStackFrameLocation(&frame, loc)
		}
		frames = append(frames, frame)

		parent := t.ops[cur]
		if parent == nil || len(parent.Inputs) == 0 {
			break
		}
		cur = digest.Digest(parent.Inputs[0].Digest)
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

func fillStackFrameLocation(frame *dap.StackFrame, loc *pb.Locations) {
	var ranges []*pb.Range
	for _, l := range loc.Locations {
		ranges = append(ranges, l.Ranges...)
	}

	frame.Line = int(ranges[0].Start.Line)
	frame.Column = int(ranges[0].Start.Character)
	frame.EndLine = int(ranges[0].End.Line)
	frame.EndColumn = int(ranges[0].End.Character)

	for _, r := range ranges {
		if frame.Line == 0 || int(r.Start.Line) < frame.Line {
			frame.Line = int(r.Start.Line)
		}
		if frame.Column == 0 || int(r.Start.Character) < frame.Column {
			frame.Column = int(r.Start.Character)
		}
		if frame.EndLine == 0 || int(r.End.Line) > frame.EndLine {
			frame.EndLine = int(r.End.Line)
		}
		if frame.EndColumn == 0 || int(r.End.Character) > frame.EndColumn {
			frame.EndColumn = int(r.End.Character)
		}
	}
}
