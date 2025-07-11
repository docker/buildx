package dap

import (
	"context"
	"sync"

	"github.com/google/go-dap"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
)

var ErrServerStopped = errors.New("dap: server stopped")

type Server struct {
	h Handler

	mu sync.RWMutex
	ch chan dap.Message

	eg     *errgroup.Group
	ctx    context.Context
	cancel context.CancelCauseFunc

	initialized bool
}

func NewServer(h Handler) *Server {
	return &Server{h: h}
}

func (s *Server) Serve(ctx context.Context, conn Conn) error {
	writeCh := make(chan dap.Message)
	s.ch = writeCh

	s.ctx, s.cancel = context.WithCancelCause(ctx)

	// Start an error group to handle server-initiated tasks.
	s.eg, _ = errgroup.WithContext(s.ctx)
	s.eg.Go(func() error {
		<-s.ctx.Done()
		return s.ctx.Err()
	})

	eg, _ := errgroup.WithContext(s.ctx)
	eg.Go(func() error {
		return s.readLoop(conn)
	})

	eg.Go(func() error {
		return s.writeLoop(conn, writeCh)
	})

	eg.Go(func() error {
		// TODO: reevaluate this logic for shutting down
		defer close(writeCh)
		err := s.eg.Wait()

		s.mu.Lock()
		s.ch = nil
		s.mu.Unlock()
		return err
	})

	return eg.Wait()
}

func (s *Server) readLoop(conn Conn) error {
	for {
		m, err := conn.RecvMsg(s.ctx)
		if err != nil {
			return nil
		}

		switch m := m.(type) {
		case dap.RequestMessage:
			if ok := s.dispatch(m); !ok {
				return nil
			}
		}
	}
}

func (s *Server) dispatch(m dap.RequestMessage) bool {
	fn := func(c Context) {
		rmsg, err := s.handleMessage(c, m)
		if err != nil {
			rmsg = &dap.Response{}
			rmsg.GetResponse().Message = err.Error()
		}
		rmsg.GetResponse().RequestSeq = m.GetSeq()
		rmsg.GetResponse().Command = m.GetRequest().Command
		rmsg.GetResponse().Success = err == nil
		c.C() <- rmsg
	}
	return s.Go(fn)
}

func (s *Server) handleMessage(c Context, m dap.Message) (dap.ResponseMessage, error) {
	switch req := m.(type) {
	case *dap.InitializeRequest:
		resp, err := s.handleInitialize(c, req)
		if err != nil {
			return nil, err
		}
		return resp, nil
	case *dap.LaunchRequest:
		return s.h.Launch.Do(c, req)
	case *dap.AttachRequest:
		return s.h.Attach.Do(c, req)
	case *dap.SetBreakpointsRequest:
		return s.h.SetBreakpoints.Do(c, req)
	case *dap.ConfigurationDoneRequest:
		return s.h.ConfigurationDone.Do(c, req)
	case *dap.DisconnectRequest:
		return s.h.Disconnect.Do(c, req)
	case *dap.TerminateRequest:
		return s.h.Terminate.Do(c, req)
	case *dap.ContinueRequest:
		return s.h.Continue.Do(c, req)
	case *dap.NextRequest:
		return s.h.Next.Do(c, req)
	case *dap.StepInRequest:
		return s.h.StepIn.Do(c, req)
	case *dap.StepOutRequest:
		return s.h.StepOut.Do(c, req)
	case *dap.RestartRequest:
		return s.h.Restart.Do(c, req)
	case *dap.ThreadsRequest:
		return s.h.Threads.Do(c, req)
	case *dap.StackTraceRequest:
		return s.h.StackTrace.Do(c, req)
	case *dap.EvaluateRequest:
		return s.h.Evaluate.Do(c, req)
	case *dap.SourceRequest:
		return s.h.Source.Do(c, req)
	default:
		return nil, errors.New("not implemented")
	}
}

func (s *Server) handleInitialize(c Context, req *dap.InitializeRequest) (*dap.InitializeResponse, error) {
	if s.initialized {
		return nil, errors.New("already initialized")
	}

	resp, err := s.h.Initialize.Do(c, req)
	if err != nil {
		return nil, err
	}
	s.initialized = true
	return resp, nil
}

func (s *Server) writeLoop(conn Conn, respCh <-chan dap.Message) error {
	var seq int
	for m := range respCh {
		switch m := m.(type) {
		case dap.RequestMessage:
			m.GetRequest().Seq = seq
			m.GetRequest().Type = "request"
		case dap.EventMessage:
			m.GetEvent().Seq = seq
			m.GetEvent().Type = "event"
		case dap.ResponseMessage:
			m.GetResponse().Seq = seq
			m.GetResponse().Type = "response"
		}
		seq++

		if err := conn.SendMsg(m); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) Go(fn func(c Context)) bool {
	acquireChannel := func() (chan<- dap.Message, bool) {
		s.mu.RLock()
		defer s.mu.RUnlock()

		return s.ch, s.ch != nil
	}

	ctx, cancel := context.WithCancelCause(s.ctx)
	c := &dispatchContext{
		Context: ctx,
		srv:     s,
	}

	started := make(chan bool, 1)
	s.eg.Go(func() error {
		var ok bool
		c.ch, ok = acquireChannel()
		started <- ok

		if c.ch == nil {
			return nil
		}

		defer cancel(context.Canceled)
		fn(c)
		return nil
	})
	return <-started
}

func (s *Server) Stop() {
	s.mu.Lock()
	s.ch = nil
	s.mu.Unlock()
	s.cancel(ErrServerStopped)
}
