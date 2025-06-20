package dap

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/docker/buildx/build"
	"github.com/google/go-dap"
	gateway "github.com/moby/buildkit/frontend/gateway/client"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
)

type Adapter struct {
	srv *Server
	eg  *errgroup.Group
	cfg build.InvokeConfig

	initialized   chan struct{}
	started       chan error
	configuration chan struct{}

	evaluateReqCh chan *evaluateRequest

	threads      map[int]*thread
	threadsMu    sync.RWMutex
	nextThreadID int
}

func New(cfg *build.InvokeConfig) *Adapter {
	d := &Adapter{
		initialized:   make(chan struct{}),
		started:       make(chan error, 1),
		configuration: make(chan struct{}),
		evaluateReqCh: make(chan *evaluateRequest),
		threads:       make(map[int]*thread),
		nextThreadID:  1,
	}
	if cfg != nil {
		d.cfg = *cfg
	}
	d.srv = NewServer(d.dapHandler())
	return d
}

func (d *Adapter) Start(ctx context.Context, conn Conn) error {
	d.eg, _ = errgroup.WithContext(ctx)
	d.eg.Go(func() error {
		return d.srv.Serve(ctx, conn)
	})

	<-d.initialized
	return <-d.started
}

func (d *Adapter) Stop() error {
	if d.eg == nil {
		return nil
	}

	d.srv.Go(func(c Context) {
		c.C() <- &dap.TerminatedEvent{
			Event: dap.Event{
				Event: "terminated",
			},
		}
		// TODO: detect exit code from threads
		// c.C() <- &dap.ExitedEvent{
		// 	Event: dap.Event{
		// 		Event: "exited",
		// 	},
		// 	Body: dap.ExitedEventBody{
		// 		ExitCode: exitCode,
		// 	},
		// }
	})
	d.srv.Stop()

	err := d.eg.Wait()
	d.eg = nil
	return err
}

func (d *Adapter) Initialize(c Context, req *dap.InitializeRequest, resp *dap.InitializeResponse) error {
	close(d.initialized)

	// Set capabilities.
	resp.Body.SupportsConfigurationDoneRequest = true
	return nil
}

func (d *Adapter) Launch(c Context, req *dap.LaunchRequest, resp *dap.LaunchResponse) error {
	d.start(c)
	return nil
}

func (d *Adapter) Disconnect(c Context, req *dap.DisconnectRequest, resp *dap.DisconnectResponse) error {
	close(d.evaluateReqCh)
	return nil
}

func (d *Adapter) start(c Context) {
	c.Go(d.launch)
}

func (d *Adapter) Continue(c Context, req *dap.ContinueRequest, resp *dap.ContinueResponse) error {
	d.threadsMu.RLock()
	t := d.threads[req.Arguments.ThreadId]
	d.threadsMu.RUnlock()

	t.Resume(c)
	return nil
}

func (d *Adapter) SetBreakpoints(c Context, req *dap.SetBreakpointsRequest, resp *dap.SetBreakpointsResponse) error {
	// TODO: implement breakpoints
	for range req.Arguments.Breakpoints {
		// Fail to create all breakpoints that were requested.
		resp.Body.Breakpoints = append(resp.Body.Breakpoints, dap.Breakpoint{
			Verified: false,
			Message:  "breakpoints unsupported",
		})
	}
	return nil
}

func (d *Adapter) ConfigurationDone(c Context, req *dap.ConfigurationDoneRequest, resp *dap.ConfigurationDoneResponse) error {
	d.configuration <- struct{}{}
	close(d.configuration)
	return nil
}

func (d *Adapter) launch(c Context) {
	// Send initialized event.
	c.C() <- &dap.InitializedEvent{
		Event: dap.Event{
			Event: "initialized",
		},
	}

	// Wait for configuration.
	select {
	case <-c.Done():
		return
	case <-d.configuration:
		// TODO: actual configuration
	}
	close(d.started)

	for {
		select {
		case <-c.Done():
			return
		case req := <-d.evaluateReqCh:
			if req == nil {
				return
			}

			t := d.newThread(c, req.name)
			started := c.Go(func(c Context) {
				defer d.deleteThread(c, t)
				defer close(req.errCh)
				req.errCh <- t.Evaluate(c, req.c, req.ref, req.meta)
			})

			if !started {
				req.errCh <- context.Canceled
				close(req.errCh)
			}
		}
	}
}

func (d *Adapter) newThread(ctx Context, name string) (t *thread) {
	d.threadsMu.Lock()
	id := d.nextThreadID
	t = &thread{
		d:    d,
		id:   id,
		name: name,
	}
	d.threads[t.id] = t
	d.nextThreadID++
	d.threadsMu.Unlock()

	ctx.C() <- &dap.ThreadEvent{
		Event: dap.Event{Event: "thread"},
		Body: dap.ThreadEventBody{
			Reason:   "started",
			ThreadId: t.id,
		},
	}
	return t
}

func (d *Adapter) getThread(id int) (t *thread) {
	d.threadsMu.Lock()
	t = d.threads[id]
	d.threadsMu.Unlock()
	return t
}

func (d *Adapter) deleteThread(ctx Context, t *thread) {
	d.threadsMu.Lock()
	delete(d.threads, t.id)
	d.threadsMu.Unlock()

	ctx.C() <- &dap.ThreadEvent{
		Event: dap.Event{Event: "thread"},
		Body: dap.ThreadEventBody{
			Reason:   "exited",
			ThreadId: t.id,
		},
	}
}

type evaluateRequest struct {
	name  string
	c     gateway.Client
	ref   gateway.Reference
	meta  map[string][]byte
	errCh chan<- error
}

func (d *Adapter) EvaluateResult(ctx context.Context, name string, c gateway.Client, res *gateway.Result) error {
	eg, _ := errgroup.WithContext(ctx)
	if res.Ref != nil {
		eg.Go(func() error {
			return d.evaluateRef(ctx, name, c, res.Ref, res.Metadata)
		})
	}

	for k, ref := range res.Refs {
		refName := fmt.Sprintf("%s (%s)", name, k)
		eg.Go(func() error {
			return d.evaluateRef(ctx, refName, c, ref, res.Metadata)
		})
	}
	return eg.Wait()
}

func (d *Adapter) evaluateRef(ctx context.Context, name string, c gateway.Client, ref gateway.Reference, meta map[string][]byte) error {
	errCh := make(chan error, 1)

	// Send a solve request to the launch routine
	// which will perform the solve in the context of the server.
	ereq := &evaluateRequest{
		name:  name,
		c:     c,
		ref:   ref,
		meta:  meta,
		errCh: errCh,
	}
	select {
	case d.evaluateReqCh <- ereq:
	case <-ctx.Done():
		return context.Cause(ctx)
	}

	// Wait for the response.
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return context.Cause(ctx)
	}
}

func (d *Adapter) Threads(c Context, req *dap.ThreadsRequest, resp *dap.ThreadsResponse) error {
	d.threadsMu.RLock()
	defer d.threadsMu.RUnlock()

	resp.Body.Threads = []dap.Thread{}
	for _, t := range d.threads {
		resp.Body.Threads = append(resp.Body.Threads, dap.Thread{
			Id:   t.id,
			Name: t.name,
		})
	}
	return nil
}

func (d *Adapter) StackTrace(c Context, req *dap.StackTraceRequest, resp *dap.StackTraceResponse) error {
	t := d.getThread(req.Arguments.ThreadId)
	if t == nil {
		return errors.Errorf("no such thread: %d", req.Arguments.ThreadId)
	}

	resp.Body.StackFrames = t.StackFrames()
	return nil
}

func (d *Adapter) evaluate(ctx context.Context, name string, c gateway.Client, res *gateway.Result) error {
	errCh := make(chan error, 1)

	started := d.srv.Go(func(ctx Context) {
		defer close(errCh)
		errCh <- d.EvaluateResult(ctx, name, c, res)
	})
	if !started {
		return context.Canceled
	}

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return context.Cause(ctx)
	}
}

func (d *Adapter) Handler() build.Handler {
	return build.Handler{
		Evaluate: d.evaluate,
	}
}

func (d *Adapter) dapHandler() Handler {
	return Handler{
		Initialize:        d.Initialize,
		Launch:            d.Launch,
		Continue:          d.Continue,
		SetBreakpoints:    d.SetBreakpoints,
		ConfigurationDone: d.ConfigurationDone,
		Threads:           d.Threads,
		StackTrace:        d.StackTrace,
	}
}

type thread struct {
	d    *Adapter
	id   int
	name string

	paused chan struct{}
	rCtx   *build.ResultHandle
	mu     sync.Mutex
}

func (t *thread) Evaluate(ctx Context, c gateway.Client, ref gateway.Reference, meta map[string][]byte) error {
	err := ref.Evaluate(ctx)
	if reason, desc := t.needsDebug(err); reason != "" {
		rCtx := build.NewResultHandle(ctx, c, ref, meta, err)
		<-t.pause(ctx, rCtx, reason, desc)
	}
	return err
}

func (t *thread) needsDebug(err error) (reason, desc string) {
	if !t.d.cfg.NeedsDebug(err) {
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

func (t *thread) pause(c Context, rCtx *build.ResultHandle, reason, desc string) <-chan struct{} {
	if t.paused == nil {
		t.paused = make(chan struct{})
	}
	t.rCtx = rCtx

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

	close(t.paused)
	t.paused = nil
}

// TODO: return a suitable stack frame for the thread.
// For now, just returns nothing.
func (t *thread) StackFrames() []dap.StackFrame {
	return []dap.StackFrame{}
}

func (d *Adapter) Out() io.Writer {
	return &adapterWriter{d}
}

type adapterWriter struct {
	*Adapter
}

func (d *adapterWriter) Write(p []byte) (n int, err error) {
	started := d.srv.Go(func(c Context) {
		c.C() <- &dap.OutputEvent{
			Event: dap.Event{Event: "output"},
			Body: dap.OutputEventBody{
				Category: "stdout",
				Output:   string(p),
			},
		}
	})

	if !started {
		return 0, io.ErrClosedPipe
	}
	return n, nil
}
