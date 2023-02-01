package remote

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/docker/buildx/build"
	"github.com/docker/buildx/controller/pb"
	"github.com/docker/buildx/util/ioset"
	"github.com/docker/buildx/version"
	controlapi "github.com/moby/buildkit/api/services/control"
	"github.com/moby/buildkit/client"
	"github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
)

type BuildFunc func(ctx context.Context, options *pb.BuildOptions, stdin io.Reader, statusChan chan *client.SolveStatus) (res *build.ResultContext, err error)

func NewServer(buildFunc BuildFunc) *Server {
	return &Server{
		buildFunc: buildFunc,
	}
}

type Server struct {
	buildFunc BuildFunc
	session   map[string]session
	sessionMu sync.Mutex
}

type session struct {
	statusChan      chan *client.SolveStatus
	result          *build.ResultContext
	inputPipe       *io.PipeWriter
	curInvokeCancel func()
	curBuildCancel  func()
}

func (m *Server) Info(ctx context.Context, req *pb.InfoRequest) (res *pb.InfoResponse, err error) {
	return &pb.InfoResponse{
		BuildxVersion: &pb.BuildxVersion{
			Package:  version.Package,
			Version:  version.Version,
			Revision: version.Revision,
		},
	}, nil
}

func (m *Server) List(ctx context.Context, req *pb.ListRequest) (res *pb.ListResponse, err error) {
	keys := make(map[string]struct{})

	m.sessionMu.Lock()
	for k := range m.session {
		keys[k] = struct{}{}
	}
	m.sessionMu.Unlock()

	var keysL []string
	for k := range keys {
		keysL = append(keysL, k)
	}
	return &pb.ListResponse{
		Keys: keysL,
	}, nil
}

func (m *Server) Disconnect(ctx context.Context, req *pb.DisconnectRequest) (res *pb.DisconnectResponse, err error) {
	key := req.Ref
	if key == "" {
		return nil, fmt.Errorf("disconnect: empty key")
	}

	m.sessionMu.Lock()
	if s, ok := m.session[key]; ok {
		if s.curBuildCancel != nil {
			s.curBuildCancel()
		}
		if s.curInvokeCancel != nil {
			s.curInvokeCancel()
		}
	}
	delete(m.session, key)
	m.sessionMu.Unlock()

	return &pb.DisconnectResponse{}, nil
}

func (m *Server) Close() error {
	m.sessionMu.Lock()
	for k := range m.session {
		if s, ok := m.session[k]; ok {
			if s.curBuildCancel != nil {
				s.curBuildCancel()
			}
			if s.curInvokeCancel != nil {
				s.curInvokeCancel()
			}
		}
	}
	m.sessionMu.Unlock()
	return nil
}

func (m *Server) Build(ctx context.Context, req *pb.BuildRequest) (*pb.BuildResponse, error) {
	ref := req.Ref
	if ref == "" {
		return nil, fmt.Errorf("build: empty key")
	}

	// Prepare status channel and session if not exists
	m.sessionMu.Lock()
	if m.session == nil {
		m.session = make(map[string]session)
	}
	s, ok := m.session[ref]
	if ok && m.session[ref].statusChan != nil {
		m.sessionMu.Unlock()
		return &pb.BuildResponse{}, fmt.Errorf("build or status ongoing or status didn't called")
	}
	statusChan := make(chan *client.SolveStatus)
	s.statusChan = statusChan
	m.session[ref] = session{statusChan: statusChan}
	m.sessionMu.Unlock()
	defer func() {
		close(statusChan)
		m.sessionMu.Lock()
		s, ok := m.session[ref]
		if ok {
			s.statusChan = nil
		}
		m.sessionMu.Unlock()
	}()

	// Prepare input stream pipe
	inR, inW := io.Pipe()
	m.sessionMu.Lock()
	if s, ok := m.session[ref]; ok {
		s.inputPipe = inW
		m.session[ref] = s
	} else {
		m.sessionMu.Unlock()
		return nil, fmt.Errorf("build: unknown key %v", ref)
	}
	m.sessionMu.Unlock()
	defer inR.Close()

	// Build the specified request
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	res, err := m.buildFunc(ctx, req.Options, inR, statusChan)
	m.sessionMu.Lock()
	if s, ok := m.session[ref]; ok {
		s.result = res
		s.curBuildCancel = cancel
		m.session[ref] = s
	} else {
		m.sessionMu.Unlock()
		return nil, fmt.Errorf("build: unknown key %v", ref)
	}
	m.sessionMu.Unlock()

	return &pb.BuildResponse{}, err
}

func (m *Server) Status(req *pb.StatusRequest, stream pb.Controller_StatusServer) error {
	ref := req.Ref
	if ref == "" {
		return fmt.Errorf("status: empty key")
	}

	// Wait and get status channel prepared by Build()
	var statusChan <-chan *client.SolveStatus
	for {
		// TODO: timeout?
		m.sessionMu.Lock()
		if _, ok := m.session[ref]; !ok || m.session[ref].statusChan == nil {
			m.sessionMu.Unlock()
			time.Sleep(time.Millisecond) // TODO: wait Build without busy loop and make it cancellable
			continue
		}
		statusChan = m.session[ref].statusChan
		m.sessionMu.Unlock()
		break
	}

	// forward status
	for ss := range statusChan {
		if ss == nil {
			break
		}
		cs := toControlStatus(ss)
		if err := stream.Send(cs); err != nil {
			return err
		}
	}

	return nil
}

func (m *Server) Input(stream pb.Controller_InputServer) (err error) {
	// Get the target ref from init message
	msg, err := stream.Recv()
	if err != nil {
		if !errors.Is(err, io.EOF) {
			return err
		}
		return nil
	}
	init := msg.GetInit()
	if init == nil {
		return fmt.Errorf("unexpected message: %T; wanted init", msg.GetInit())
	}
	ref := init.Ref
	if ref == "" {
		return fmt.Errorf("input: no ref is provided")
	}

	// Wait and get input stream pipe prepared by Build()
	var inputPipeW *io.PipeWriter
	for {
		// TODO: timeout?
		m.sessionMu.Lock()
		if _, ok := m.session[ref]; !ok || m.session[ref].inputPipe == nil {
			m.sessionMu.Unlock()
			time.Sleep(time.Millisecond) // TODO: wait Build without busy loop and make it cancellable
			continue
		}
		inputPipeW = m.session[ref].inputPipe
		m.sessionMu.Unlock()
		break
	}

	// Forward input stream
	eg, ctx := errgroup.WithContext(context.TODO())
	done := make(chan struct{})
	msgCh := make(chan *pb.InputMessage)
	eg.Go(func() error {
		defer close(msgCh)
		for {
			msg, err := stream.Recv()
			if err != nil {
				if !errors.Is(err, io.EOF) {
					return err
				}
				return nil
			}
			select {
			case msgCh <- msg:
			case <-done:
				return nil
			case <-ctx.Done():
				return nil
			}
		}
	})
	eg.Go(func() (retErr error) {
		defer close(done)
		defer func() {
			if retErr != nil {
				inputPipeW.CloseWithError(retErr)
				return
			}
			inputPipeW.Close()
		}()
		for {
			var msg *pb.InputMessage
			select {
			case msg = <-msgCh:
			case <-ctx.Done():
				return fmt.Errorf("canceled: %w", ctx.Err())
			}
			if msg == nil {
				return nil
			}
			if data := msg.GetData(); data != nil {
				if len(data.Data) > 0 {
					_, err := inputPipeW.Write(data.Data)
					if err != nil {
						return err
					}
				}
				if data.EOF {
					return nil
				}
			}
		}
	})

	return eg.Wait()
}

func (m *Server) Invoke(srv pb.Controller_InvokeServer) error {
	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()
	containerIn, containerOut := ioset.Pipe()
	waitInvokeDoneCh := make(chan struct{})
	var cancelOnce sync.Once
	curInvokeCancel := func() {
		cancelOnce.Do(func() { containerOut.Close(); containerIn.Close(); cancel() })
		<-waitInvokeDoneCh
	}
	defer curInvokeCancel()

	var cfg *pb.ContainerConfig
	var resultCtx *build.ResultContext
	initDoneCh := make(chan struct{})
	initErrCh := make(chan error)
	eg, egCtx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		return serveIO(egCtx, srv, func(initMessage *pb.InitMessage) (retErr error) {
			defer func() {
				if retErr != nil {
					initErrCh <- retErr
				}
				close(initDoneCh)
			}()
			ref := initMessage.Ref
			cfg = initMessage.ContainerConfig

			// Register cancel callback
			m.sessionMu.Lock()
			if s, ok := m.session[ref]; ok {
				if cancel := s.curInvokeCancel; cancel != nil {
					logrus.Warnf("invoke: cancelling ongoing invoke of %q", ref)
					cancel()
				}
				s.curInvokeCancel = curInvokeCancel
				m.session[ref] = s
			} else {
				m.sessionMu.Unlock()
				return fmt.Errorf("invoke: unknown key %v", ref)
			}
			m.sessionMu.Unlock()

			// Get the target result to invoke a container from
			m.sessionMu.Lock()
			if _, ok := m.session[ref]; !ok || m.session[ref].result == nil {
				m.sessionMu.Unlock()
				return fmt.Errorf("unknown reference: %q", ref)
			}
			resultCtx = m.session[ref].result
			m.sessionMu.Unlock()
			return nil
		}, &ioServerConfig{
			stdin:  containerOut.Stdin,
			stdout: containerOut.Stdout,
			stderr: containerOut.Stderr,
			// TODO: signal, resize
		})
	})
	eg.Go(func() error {
		defer containerIn.Close()
		defer cancel()
		select {
		case <-initDoneCh:
		case err := <-initErrCh:
			return err
		}
		if cfg == nil {
			return fmt.Errorf("no container config is provided")
		}
		if resultCtx == nil {
			return fmt.Errorf("no result is provided")
		}
		ccfg := build.ContainerConfig{
			ResultCtx:  resultCtx,
			Entrypoint: cfg.Entrypoint,
			Cmd:        cfg.Cmd,
			Env:        cfg.Env,
			Tty:        cfg.Tty,
			Stdin:      containerIn.Stdin,
			Stdout:     containerIn.Stdout,
			Stderr:     containerIn.Stderr,
		}
		if !cfg.NoUser {
			ccfg.User = &cfg.User
		}
		if !cfg.NoCwd {
			ccfg.Cwd = &cfg.Cwd
		}
		return build.Invoke(egCtx, ccfg)
	})
	err := eg.Wait()
	close(waitInvokeDoneCh)
	curInvokeCancel()

	return err
}

func toControlStatus(s *client.SolveStatus) *pb.StatusResponse {
	resp := pb.StatusResponse{}
	for _, v := range s.Vertexes {
		resp.Vertexes = append(resp.Vertexes, &controlapi.Vertex{
			Digest:        v.Digest,
			Inputs:        v.Inputs,
			Name:          v.Name,
			Started:       v.Started,
			Completed:     v.Completed,
			Error:         v.Error,
			Cached:        v.Cached,
			ProgressGroup: v.ProgressGroup,
		})
	}
	for _, v := range s.Statuses {
		resp.Statuses = append(resp.Statuses, &controlapi.VertexStatus{
			ID:        v.ID,
			Vertex:    v.Vertex,
			Name:      v.Name,
			Total:     v.Total,
			Current:   v.Current,
			Timestamp: v.Timestamp,
			Started:   v.Started,
			Completed: v.Completed,
		})
	}
	for _, v := range s.Logs {
		resp.Logs = append(resp.Logs, &controlapi.VertexLog{
			Vertex:    v.Vertex,
			Stream:    int64(v.Stream),
			Msg:       v.Data,
			Timestamp: v.Timestamp,
		})
	}
	for _, v := range s.Warnings {
		resp.Warnings = append(resp.Warnings, &controlapi.VertexWarning{
			Vertex: v.Vertex,
			Level:  int64(v.Level),
			Short:  v.Short,
			Detail: v.Detail,
			Url:    v.URL,
			Info:   v.SourceInfo,
			Ranges: v.Range,
		})
	}
	return &resp
}
