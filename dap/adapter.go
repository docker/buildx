package dap

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"sync"
	"sync/atomic"

	"github.com/docker/buildx/build"
	"github.com/google/go-dap"
	gateway "github.com/moby/buildkit/frontend/gateway/client"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
)

type Adapter[T any] struct {
	srv *Server
	eg  *errgroup.Group
	cfg build.InvokeConfig

	initialized   chan struct{}
	started       chan launchResponse[T]
	configuration chan struct{}

	evaluateReqCh chan *evaluateRequest

	threads      map[int]*thread
	threadsMu    sync.RWMutex
	nextThreadID int

	sourceMap sourceMap
	idPool    *idPool
}

func New[T any](cfg *build.InvokeConfig) *Adapter[T] {
	d := &Adapter[T]{
		initialized:   make(chan struct{}),
		started:       make(chan launchResponse[T], 1),
		configuration: make(chan struct{}),
		evaluateReqCh: make(chan *evaluateRequest),
		threads:       make(map[int]*thread),
		nextThreadID:  1,
		idPool:        new(idPool),
	}
	if cfg != nil {
		d.cfg = *cfg
	}
	d.srv = NewServer(d.dapHandler())
	return d
}

func (d *Adapter[T]) Start(ctx context.Context, conn Conn) (T, error) {
	d.eg, _ = errgroup.WithContext(ctx)
	d.eg.Go(func() error {
		return d.srv.Serve(ctx, conn)
	})

	<-d.initialized

	resp, ok := <-d.started
	if !ok {
		resp.Error = context.Canceled
	}
	return resp.Config, resp.Error
}

func (d *Adapter[T]) Stop() error {
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

func (d *Adapter[T]) Initialize(c Context, req *dap.InitializeRequest, resp *dap.InitializeResponse) error {
	close(d.initialized)

	// Set capabilities.
	resp.Body.SupportsConfigurationDoneRequest = true
	return nil
}

type launchResponse[T any] struct {
	Config T
	Error  error
}

func (d *Adapter[T]) Launch(c Context, req *dap.LaunchRequest, resp *dap.LaunchResponse) error {
	defer close(d.started)

	var cfg T
	if err := json.Unmarshal(req.Arguments, &cfg); err != nil {
		d.started <- launchResponse[T]{Error: err}
		return err
	}

	d.start(c)

	d.started <- launchResponse[T]{Config: cfg}
	return nil
}

func (d *Adapter[T]) Disconnect(c Context, req *dap.DisconnectRequest, resp *dap.DisconnectResponse) error {
	close(d.evaluateReqCh)
	return nil
}

func (d *Adapter[T]) start(c Context) {
	c.Go(d.launch)
}

func (d *Adapter[T]) Continue(c Context, req *dap.ContinueRequest, resp *dap.ContinueResponse) error {
	d.threadsMu.RLock()
	t := d.threads[req.Arguments.ThreadId]
	d.threadsMu.RUnlock()

	t.Continue()
	return nil
}

func (d *Adapter[T]) Next(c Context, req *dap.NextRequest, resp *dap.NextResponse) error {
	d.threadsMu.RLock()
	t := d.threads[req.Arguments.ThreadId]
	d.threadsMu.RUnlock()

	t.Next()
	return nil
}

func (d *Adapter[T]) SetBreakpoints(c Context, req *dap.SetBreakpointsRequest, resp *dap.SetBreakpointsResponse) error {
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

func (d *Adapter[T]) ConfigurationDone(c Context, req *dap.ConfigurationDoneRequest, resp *dap.ConfigurationDoneResponse) error {
	d.configuration <- struct{}{}
	close(d.configuration)
	return nil
}

func (d *Adapter[T]) launch(c Context) {
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

	for {
		select {
		case <-c.Done():
			return
		case req, ok := <-d.evaluateReqCh:
			if !ok {
				return
			}

			t := d.newThread(c, req.name)
			started := c.Go(func(c Context) {
				defer d.deleteThread(c, t)
				defer close(req.errCh)
				req.errCh <- t.Evaluate(c, req.c, req.ref, req.meta, req.inputs)
			})

			if !started {
				req.errCh <- context.Canceled
				close(req.errCh)
			}
		}
	}
}

func (d *Adapter[T]) newThread(ctx Context, name string) (t *thread) {
	d.threadsMu.Lock()
	id := d.nextThreadID
	t = &thread{
		id:        id,
		name:      name,
		sourceMap: &d.sourceMap,
		idPool:    d.idPool,
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

func (d *Adapter[T]) getThread(id int) (t *thread) {
	d.threadsMu.Lock()
	t = d.threads[id]
	d.threadsMu.Unlock()
	return t
}

func (d *Adapter[T]) deleteThread(ctx Context, t *thread) {
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
	name   string
	c      gateway.Client
	ref    gateway.Reference
	meta   map[string][]byte
	inputs build.Inputs
	errCh  chan<- error
}

func (d *Adapter[T]) EvaluateResult(ctx context.Context, name string, c gateway.Client, res *gateway.Result, inputs build.Inputs) error {
	eg, _ := errgroup.WithContext(ctx)
	if res.Ref != nil {
		eg.Go(func() error {
			return d.evaluateRef(ctx, name, c, res.Ref, res.Metadata, inputs)
		})
	}

	for k, ref := range res.Refs {
		refName := fmt.Sprintf("%s (%s)", name, k)
		eg.Go(func() error {
			return d.evaluateRef(ctx, refName, c, ref, res.Metadata, inputs)
		})
	}
	return eg.Wait()
}

func (d *Adapter[T]) evaluateRef(ctx context.Context, name string, c gateway.Client, ref gateway.Reference, meta map[string][]byte, inputs build.Inputs) error {
	errCh := make(chan error, 1)

	// Send a solve request to the launch routine
	// which will perform the solve in the context of the server.
	ereq := &evaluateRequest{
		name:   name,
		c:      c,
		ref:    ref,
		meta:   meta,
		inputs: inputs,
		errCh:  errCh,
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

func (d *Adapter[T]) Threads(c Context, req *dap.ThreadsRequest, resp *dap.ThreadsResponse) error {
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

func (d *Adapter[T]) StackTrace(c Context, req *dap.StackTraceRequest, resp *dap.StackTraceResponse) error {
	t := d.getThread(req.Arguments.ThreadId)
	if t == nil {
		return errors.Errorf("no such thread: %d", req.Arguments.ThreadId)
	}

	resp.Body.StackFrames = t.StackTrace()
	return nil
}

func (d *Adapter[T]) Source(c Context, req *dap.SourceRequest, resp *dap.SourceResponse) error {
	fname := req.Arguments.Source.Path

	dt, ok := d.sourceMap.Get(fname)
	if !ok {
		return errors.Errorf("file not found: %s", fname)
	}

	resp.Body.Content = string(dt)
	return nil
}

func (d *Adapter[T]) evaluate(ctx context.Context, name string, c gateway.Client, res *gateway.Result, opt build.Options) error {
	errCh := make(chan error, 1)

	started := d.srv.Go(func(ctx Context) {
		defer close(errCh)
		errCh <- d.EvaluateResult(ctx, name, c, res, opt.Inputs)
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

func (d *Adapter[T]) Handler() build.Handler {
	return build.Handler{
		Evaluate: d.evaluate,
	}
}

func (d *Adapter[T]) dapHandler() Handler {
	return Handler{
		Initialize:        d.Initialize,
		Launch:            d.Launch,
		Continue:          d.Continue,
		Next:              d.Next,
		SetBreakpoints:    d.SetBreakpoints,
		ConfigurationDone: d.ConfigurationDone,
		Disconnect:        d.Disconnect,
		Threads:           d.Threads,
		StackTrace:        d.StackTrace,
		Source:            d.Source,
	}
}

func (d *Adapter[T]) Out() io.Writer {
	return &adapterWriter[T]{d}
}

type adapterWriter[T any] struct {
	*Adapter[T]
}

func (d *adapterWriter[T]) Write(p []byte) (n int, err error) {
	started := d.srv.Go(func(c Context) {
		<-d.initialized

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

type idPool struct {
	next atomic.Int64
}

func (p *idPool) Get() int64 {
	return p.next.Add(1)
}

func (p *idPool) Put(x int64) {
	// noop
}

type sourceMap struct {
	m sync.Map
}

func (s *sourceMap) Put(c Context, fname string, dt []byte) {
	for {
		old, loaded := s.m.LoadOrStore(fname, dt)
		if !loaded {
			c.C() <- &dap.LoadedSourceEvent{
				Event: dap.Event{Event: "loadedSource"},
				Body: dap.LoadedSourceEventBody{
					Reason: "new",
					Source: dap.Source{
						Name: path.Base(fname),
						Path: fname,
					},
				},
			}
		}

		if bytes.Equal(old.([]byte), dt) {
			// Nothing to do.
			return
		}

		if s.m.CompareAndSwap(fname, old, dt) {
			c.C() <- &dap.LoadedSourceEvent{
				Event: dap.Event{Event: "loadedSource"},
				Body: dap.LoadedSourceEventBody{
					Reason: "changed",
					Source: dap.Source{
						Name: path.Base(fname),
						Path: fname,
					},
				},
			}
		}
	}
}

func (s *sourceMap) Get(fname string) ([]byte, bool) {
	v, ok := s.m.Load(fname)
	if !ok {
		return nil, false
	}
	return v.([]byte), true
}
