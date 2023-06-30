package utils

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/docker/buildx/controller/control"
	controllerapi "github.com/docker/buildx/controller/pb"
	"github.com/docker/buildx/monitor/types"
	"github.com/docker/buildx/util/progress"
	"github.com/docker/buildx/util/walker"
	"github.com/moby/buildkit/client/llb"
	solverpb "github.com/moby/buildkit/solver/pb"
	"github.com/pkg/errors"
)

func IsProcessID(ctx context.Context, c control.BuildxController, curRef, ref string) (bool, error) {
	infos, err := c.ListProcesses(ctx, curRef)
	if err != nil {
		return false, err
	}
	for _, p := range infos {
		if p.ProcessID == ref {
			return true, nil
		}
	}
	return false, nil
}

func PrintLines(w io.Writer, source *solverpb.SourceInfo, positions []solverpb.Range, bps *walker.Breakpoints, before, after int, all bool) {
	fmt.Fprintf(w, "Filename: %q\n", source.Filename)
	scanner := bufio.NewScanner(bytes.NewReader(source.Data))
	lastLinePrinted := false
	firstPrint := true
	for i := 1; scanner.Scan(); i++ {
		print := false
		target := false
		if len(positions) == 0 {
			print = true
		} else {
			for _, r := range positions {
				if all || int(r.Start.Line)-before <= i && i <= int(r.End.Line)+after {
					print = true
					if int(r.Start.Line) <= i && i <= int(r.End.Line) {
						target = true
						break
					}
				}
			}
		}

		if !print {
			lastLinePrinted = false
			continue
		}
		if !lastLinePrinted && !firstPrint {
			fmt.Fprintln(w, "----------------")
		}

		prefix := " "
		bps.ForEach(func(key string, b walker.Breakpoint) bool {
			if b.IsMarked(int64(i)) {
				prefix = "*"
				return false
			}
			return true
		})
		prefix2 := "  "
		if target {
			prefix2 = "=>"
		}
		fmt.Fprintln(w, prefix+prefix2+fmt.Sprintf("%4d| ", i)+scanner.Text())
		lastLinePrinted = true
		firstPrint = false
	}
}

func IsSameDefinition(a *solverpb.Definition, b *solverpb.Definition) bool {
	ctx := context.TODO()
	opA, err := llb.NewDefinitionOp(a)
	if err != nil {
		return false
	}
	dgstA, _, _, _, err := llb.NewState(opA).Output().Vertex(ctx, nil).Marshal(ctx, nil)
	if err != nil {
		return false
	}
	opB, err := llb.NewDefinitionOp(b)
	if err != nil {
		return false
	}
	dgstB, _, _, _, err := llb.NewState(opB).Output().Vertex(ctx, nil).Marshal(ctx, nil)
	if err != nil {
		return false
	}
	return dgstA.String() == dgstB.String()
}

func SetDefaultBreakpoints(bps *walker.Breakpoints) {
	bps.ClearAll()
	bps.Add("stopOnEntry", walker.NewStopOnEntryBreakpoint()) // always enabled
	bps.Add("stopOnErr", walker.NewOnErrorBreakpoint())
}

func NewWalkerController(m types.Monitor, stdout io.WriteCloser, invokeConfig controllerapi.InvokeConfig, progress *progress.Printer, def *solverpb.Definition) *walker.Controller {
	bps := walker.NewBreakpoints()
	SetDefaultBreakpoints(bps)
	return walker.NewController(def, bps, func(ctx context.Context, bCtx *walker.BreakContext) error {
		var keys []string
		for k := range bCtx.Hits {
			keys = append(keys, k)
		}
		fmt.Fprintf(stdout, "Break at %+v\n", keys)
		PrintLines(stdout, bCtx.Definition.Source.Infos[0], bCtx.Cursors, bCtx.Breakpoints, 0, 0, true)
		m.Rollback(ctx, invokeConfig)
		return nil
	}, func(ctx context.Context, st llb.State) error {
		d, err := st.Marshal(ctx)
		if err != nil {
			return errors.Errorf("solve: failed to marshal definition: %v", err)
		}
		progress.Unpause()
		err = m.Solve(ctx, m.AttachedSessionID(), d.ToPB(), progress)
		progress.Pause()
		if err != nil {
			fmt.Fprintf(stdout, "failed during walk: %v\n", err)
		}
		return err
	}, func(err error) {
		if err == nil {
			fmt.Fprintf(stdout, "walker finished\n")
		} else {
			fmt.Fprintf(stdout, "walker finished with error %v\n", err)
		}
	})
}
