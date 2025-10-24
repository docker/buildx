package dap

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/docker/buildx/build"
	"github.com/docker/buildx/dap/common"
	"github.com/google/go-dap"
	gateway "github.com/moby/buildkit/frontend/gateway/client"
	"github.com/moby/buildkit/solver/pb"
	"github.com/opencontainers/go-digest"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
)

type Adapter[C LaunchConfig] struct {
	srv *Server
	eg  *errgroup.Group
	cfg common.Config

	initialized   chan struct{}
	started       chan launchResponse[C]
	configuration chan struct{}
	supportsExec  bool

	evaluateReqCh chan *evaluateRequest

	threads      map[int]*thread
	threadsMu    sync.RWMutex
	nextThreadID int

	sharedState
}

type sharedState struct {
	breakpointMap *breakpointMap
	sourceMap     *sourceMap
	idPool        *idPool
	sh            *shell
}

func New[C LaunchConfig]() *Adapter[C] {
	d := &Adapter[C]{
		initialized:   make(chan struct{}),
		started:       make(chan launchResponse[C], 1),
		configuration: make(chan struct{}),
		evaluateReqCh: make(chan *evaluateRequest),
		threads:       make(map[int]*thread),
		nextThreadID:  1,
		sharedState: sharedState{
			breakpointMap: newBreakpointMap(),
			sourceMap:     new(sourceMap),
			idPool:        new(idPool),
			sh:            newShell(),
		},
	}
	d.srv = NewServer(d.dapHandler())
	return d
}

func (d *Adapter[C]) Start(ctx context.Context, conn Conn) (C, error) {
	d.eg, _ = errgroup.WithContext(ctx)
	d.eg.Go(func() error {
		return d.srv.Serve(ctx, conn)
	})

	<-d.initialized

	resp, ok := <-d.started
	if !ok {
		resp.Error = context.Canceled
	}
	d.cfg = resp.Config.GetConfig()
	return resp.Config, resp.Error
}

func (d *Adapter[C]) Stop() error {
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

func (d *Adapter[C]) Initialize(c Context, req *dap.InitializeRequest, resp *dap.InitializeResponse) error {
	close(d.initialized)

	// Set parameters based on passed client capabilities.
	d.supportsExec = req.Arguments.SupportsRunInTerminalRequest

	// Set capabilities.
	resp.Body.SupportsConfigurationDoneRequest = true
	return nil
}

type launchResponse[C any] struct {
	Config C
	Error  error
}

func (d *Adapter[C]) Launch(c Context, req *dap.LaunchRequest, resp *dap.LaunchResponse) error {
	defer close(d.started)

	var cfg C
	if err := json.Unmarshal(req.Arguments, &cfg); err != nil {
		d.started <- launchResponse[C]{Error: err}
		return err
	}

	d.start(c)

	d.started <- launchResponse[C]{Config: cfg}
	return nil
}

func (d *Adapter[C]) Disconnect(c Context, req *dap.DisconnectRequest, resp *dap.DisconnectResponse) error {
	close(d.evaluateReqCh)
	return nil
}

func (d *Adapter[C]) start(c Context) {
	c.Go(d.launch)
}

func (d *Adapter[C]) Continue(c Context, req *dap.ContinueRequest, resp *dap.ContinueResponse) error {
	d.threadsMu.RLock()
	t := d.threads[req.Arguments.ThreadId]
	d.threadsMu.RUnlock()

	t.Continue()
	return nil
}

func (d *Adapter[C]) Next(c Context, req *dap.NextRequest, resp *dap.NextResponse) error {
	d.threadsMu.RLock()
	t := d.threads[req.Arguments.ThreadId]
	d.threadsMu.RUnlock()

	t.Next()
	return nil
}

func (d *Adapter[C]) StepIn(c Context, req *dap.StepInRequest, resp *dap.StepInResponse) error {
	d.threadsMu.RLock()
	t := d.threads[req.Arguments.ThreadId]
	d.threadsMu.RUnlock()

	t.StepIn()
	return nil
}

func (d *Adapter[C]) StepOut(c Context, req *dap.StepOutRequest, resp *dap.StepOutResponse) error {
	d.threadsMu.RLock()
	t := d.threads[req.Arguments.ThreadId]
	d.threadsMu.RUnlock()

	t.StepOut()
	return nil
}

func (d *Adapter[C]) SetBreakpoints(c Context, req *dap.SetBreakpointsRequest, resp *dap.SetBreakpointsResponse) error {
	resp.Body.Breakpoints = d.breakpointMap.Set(req.Arguments.Source.Path, req.Arguments.Breakpoints)
	return nil
}

func (d *Adapter[C]) ConfigurationDone(c Context, req *dap.ConfigurationDoneRequest, resp *dap.ConfigurationDoneResponse) error {
	d.configuration <- struct{}{}
	close(d.configuration)
	return nil
}

func (d *Adapter[C]) launch(c Context) {
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
				req.errCh <- t.Evaluate(c, req.c, req.ref, req.meta, req.inputs, d.cfg)
			})

			if !started {
				req.errCh <- context.Canceled
				close(req.errCh)
			}
		}
	}
}

func (d *Adapter[C]) newThread(ctx Context, name string) (t *thread) {
	d.threadsMu.Lock()
	id := d.nextThreadID
	t = &thread{
		id:          id,
		name:        name,
		sharedState: d.sharedState,
		variables:   newVariableReferences(),
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

func (d *Adapter[C]) getThread(id int) (t *thread) {
	d.threadsMu.Lock()
	t = d.threads[id]
	d.threadsMu.Unlock()
	return t
}

func (d *Adapter[C]) deleteThread(ctx Context, t *thread) {
	d.threadsMu.Lock()
	if t := d.threads[t.id]; t != nil {
		if t.variables != nil {
			t.variables.Reset()
		}
	}
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

func (d *Adapter[T]) getThreadByFrameID(id int) (t *thread) {
	d.threadsMu.RLock()
	defer d.threadsMu.RUnlock()

	for _, t := range d.threads {
		if t.hasFrame(id) {
			return t
		}
	}
	return nil
}

type evaluateRequest struct {
	name   string
	c      gateway.Client
	ref    gateway.Reference
	meta   map[string][]byte
	inputs build.Inputs
	errCh  chan<- error
}

func (d *Adapter[C]) EvaluateResult(ctx context.Context, name string, c gateway.Client, res *gateway.Result, inputs build.Inputs) error {
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

func (d *Adapter[C]) evaluateRef(ctx context.Context, name string, c gateway.Client, ref gateway.Reference, meta map[string][]byte, inputs build.Inputs) error {
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

func (d *Adapter[C]) Threads(c Context, req *dap.ThreadsRequest, resp *dap.ThreadsResponse) error {
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

func (d *Adapter[C]) StackTrace(c Context, req *dap.StackTraceRequest, resp *dap.StackTraceResponse) error {
	t := d.getThread(req.Arguments.ThreadId)
	if t == nil {
		return errors.Errorf("no such thread: %d", req.Arguments.ThreadId)
	}

	resp.Body.StackFrames = t.StackTrace()
	return nil
}

func (d *Adapter[C]) Scopes(c Context, req *dap.ScopesRequest, resp *dap.ScopesResponse) error {
	t := d.getThreadByFrameID(req.Arguments.FrameId)
	if t == nil {
		return errors.Errorf("no such frame id: %d", req.Arguments.FrameId)
	}

	resp.Body.Scopes = t.Scopes(req.Arguments.FrameId)
	for i, s := range resp.Body.Scopes {
		resp.Body.Scopes[i].VariablesReference = (t.id << 24) | s.VariablesReference
	}
	return nil
}

func (d *Adapter[C]) Variables(c Context, req *dap.VariablesRequest, resp *dap.VariablesResponse) error {
	tid := req.Arguments.VariablesReference >> 24

	t := d.getThread(tid)
	if t == nil {
		return errors.Errorf("no such thread: %d", tid)
	}

	varRef := req.Arguments.VariablesReference & ((1 << 24) - 1)
	resp.Body.Variables = t.Variables(varRef)
	for i, ref := range resp.Body.Variables {
		if ref.VariablesReference > 0 {
			resp.Body.Variables[i].VariablesReference = (tid << 24) | ref.VariablesReference
		}
	}
	return nil
}

func (d *Adapter[C]) Source(c Context, req *dap.SourceRequest, resp *dap.SourceResponse) error {
	fname := req.Arguments.Source.Path

	dt, ok := d.sourceMap.Get(fname)
	if !ok {
		return errors.Errorf("file not found: %s", fname)
	}

	resp.Body.Content = string(dt)
	return nil
}

func (d *Adapter[C]) evaluate(ctx context.Context, name string, c gateway.Client, res *gateway.Result, opt build.Options) error {
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

func (d *Adapter[C]) Handler() build.Handler {
	return build.Handler{
		Evaluate: d.evaluate,
	}
}

func (d *Adapter[C]) dapHandler() Handler {
	return Handler{
		Initialize:        d.Initialize,
		Launch:            d.Launch,
		Continue:          d.Continue,
		Next:              d.Next,
		StepIn:            d.StepIn,
		StepOut:           d.StepOut,
		SetBreakpoints:    d.SetBreakpoints,
		ConfigurationDone: d.ConfigurationDone,
		Disconnect:        d.Disconnect,
		Threads:           d.Threads,
		StackTrace:        d.StackTrace,
		Scopes:            d.Scopes,
		Variables:         d.Variables,
		Evaluate:          d.Evaluate,
		Source:            d.Source,
	}
}

func (d *Adapter[C]) Out() io.Writer {
	return &adapterWriter[C]{d}
}

type adapterWriter[C LaunchConfig] struct {
	*Adapter[C]
}

func (d *adapterWriter[C]) Write(p []byte) (n int, err error) {
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

type breakpointMap struct {
	byPath map[string][]dap.Breakpoint
	mu     sync.RWMutex

	nextID atomic.Int64
}

func newBreakpointMap() *breakpointMap {
	return &breakpointMap{
		byPath: make(map[string][]dap.Breakpoint),
	}
}

func (b *breakpointMap) Set(fname string, sbps []dap.SourceBreakpoint) (breakpoints []dap.Breakpoint) {
	b.mu.Lock()
	defer b.mu.Unlock()
	// explicitly initialize breakpoints so that
	// we do not send a null back in the JSON if there are no breakpoints
	breakpoints = []dap.Breakpoint{}

	prev := b.byPath[fname]
	for _, sbp := range sbps {
		index := slices.IndexFunc(prev, func(e dap.Breakpoint) bool {
			return sbp.Line >= e.Line && sbp.Line <= e.EndLine && sbp.Column >= e.Column && sbp.Column <= e.EndColumn
		})

		var bp dap.Breakpoint
		if index >= 0 {
			bp = prev[index]
		} else {
			bp = dap.Breakpoint{
				Id:        int(b.nextID.Add(1)),
				Line:      sbp.Line,
				EndLine:   sbp.Line,
				Column:    sbp.Column,
				EndColumn: sbp.Column,
			}
		}
		breakpoints = append(breakpoints, bp)
	}
	b.byPath[fname] = breakpoints
	return breakpoints
}

func (b *breakpointMap) Intersect(ctx Context, src *pb.Source, ws string) map[digest.Digest]int {
	b.mu.Lock()
	defer b.mu.Unlock()

	digests := make(map[digest.Digest]int)

	for dgst, locs := range src.Locations {
		if id := b.intersect(ctx, src, locs, ws); id > 0 {
			digests[digest.Digest(dgst)] = id
		}
	}
	return digests
}

func (b *breakpointMap) intersect(ctx Context, src *pb.Source, locs *pb.Locations, ws string) int {
	overlaps := func(r *pb.Range, bp *dap.Breakpoint) bool {
		return r.Start.Line <= int32(bp.Line) && r.Start.Character <= int32(bp.Column) && r.End.Line >= int32(bp.EndLine) && r.End.Character >= int32(bp.EndColumn)
	}

	for _, loc := range locs.Locations {
		if len(loc.Ranges) == 0 {
			continue
		}
		r := loc.Ranges[0]

		info := src.Infos[loc.SourceIndex]
		fname := filepath.Join(ws, info.Filename)

		bps := b.byPath[fname]
		if len(bps) == 0 {
			// No breakpoints for this file.
			continue
		}

		for i, bp := range bps {
			if !overlaps(r, &bp) {
				continue
			}

			if !bp.Verified {
				bp.Line = int(r.Start.Line)
				bp.EndLine = int(r.End.Line)
				bp.Column = int(r.Start.Character)
				bp.EndColumn = int(r.End.Character)
				bp.Verified = true

				ctx.C() <- &dap.BreakpointEvent{
					Event: dap.Event{Event: "breakpoint"},
					Body: dap.BreakpointEventBody{
						Reason:     "changed",
						Breakpoint: bp,
					},
				}
				bps[i] = bp
			}
			return bp.Id
		}
	}
	return 0
}
