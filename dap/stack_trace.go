package dap

import (
	"context"

	"github.com/google/go-dap"
	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/solver/pb"
	"github.com/opencontainers/go-digest"
)

func (t *thread) newStackFrame() dap.StackFrame {
	return dap.StackFrame{
		Id: int(t.d.idPool.Get()),
	}
}

func (t *thread) makeStackTrace() []dap.StackFrame {
	def, head, err := t.rCtx.ToDef(context.Background())
	if err != nil {
		return []dap.StackFrame{}
	}

	ops := make(map[digest.Digest]*pb.Op)
	for _, dt := range def.Def {
		dgst := digest.FromBytes(dt)

		var op pb.Op
		if err := op.UnmarshalVT(dt); err != nil {
			return []dap.StackFrame{}
		}
		ops[dgst] = &op
	}

	var frames []dap.StackFrame
	for cur := head; ; {
		frame := t.newStackFrame()
		if meta, ok := def.Metadata[cur]; ok {
			fillStackFrameMetadata(&frame, meta)
		}
		if loc, ok := def.Source.Locations[string(cur)]; ok {
			fillStackFrameLocation(&frame, loc)
		}
		frames = append(frames, frame)

		parent := ops[cur]
		if parent == nil || len(parent.Inputs) == 0 {
			return frames
		}
		cur = digest.Digest(parent.Inputs[0].Digest)
	}
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
