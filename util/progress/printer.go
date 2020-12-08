package progress

import (
	"context"
	"os"

	"github.com/containerd/console"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/util/progress/progressui"
)

type Printer struct {
	status chan *client.SolveStatus
	done   <-chan struct{}
	err    error
}

func (p *Printer) Wait() error {
	close(p.status)
	<-p.done
	return p.err
}

func (p *Printer) Write(s *client.SolveStatus) {
	p.status <- s
}

func NewPrinter(ctx context.Context, out console.File, mode string) *Printer {
	statusCh := make(chan *client.SolveStatus)
	doneCh := make(chan struct{})

	pw := &Printer{
		status: statusCh,
		done:   doneCh,
	}

	if v := os.Getenv("BUILDKIT_PROGRESS"); v != "" && mode == "auto" {
		mode = v
	}

	go func() {
		var c console.Console
		if cons, err := console.ConsoleFromFile(out); err == nil && (mode == "auto" || mode == "tty") {
			c = cons
		}
		// not using shared context to not disrupt display but let is finish reporting errors
		pw.err = progressui.DisplaySolveStatus(ctx, "", c, out, statusCh)
		close(doneCh)
	}()
	return pw
}
