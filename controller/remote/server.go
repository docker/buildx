package remote

import (
	"context"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/docker/buildx/build"
	controllererrors "github.com/docker/buildx/controller/errdefs"
	"github.com/docker/buildx/controller/pb"
	"github.com/docker/buildx/controller/processes"
	"github.com/docker/buildx/util/ioset"
	"github.com/docker/buildx/util/progress"
	"github.com/docker/buildx/version"
	"github.com/moby/buildkit/client"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
)

type BuildFunc func(ctx context.Context, options *pb.BuildOptions, stdin io.Reader, progress progress.Writer) (resp *client.SolveResponse, res *build.ResultHandle, err error)

func NewServer(buildFunc BuildFunc) *Server {
	return &Server{
		buildFunc: buildFunc,
	}
}

type Server struct {
	buildFunc BuildFunc
	session   map[string]*session
	sessionMu sync.Mutex
}

type session struct {
	buildOnGoing atomic.Bool
	statusChan   chan *pb.StatusResponse
	cancelBuild  func()
	buildOptions *pb.BuildOptions
	inputPipe    *io.PipeWriter

	result *build.ResultHandle

	processes *processes.Manager
}

func (s *session) cancelRunningProcesses() {
	s.processes.CancelRunningProcesses()
}

func (m *Server) ListProcesses(ctx context.Context, req *pb.ListProcessesRequest) (res *pb.ListProcessesResponse, err error) {
	m.sessionMu.Lock()
	defer m.sessionMu.Unlock()
	s, ok := m.session[req.Ref]
	if !ok {
		return nil, errors.Errorf("unknown ref %q", req.Ref)
	}
	res = new(pb.ListProcessesResponse)
	res.Infos = append(res.Infos, s.processes.ListProcesses()...)
	return res, nil
}

func (m *Server) DisconnectProcess(ctx context.Context, req *pb.DisconnectProcessRequest) (res *pb.DisconnectProcessResponse, err error) {
	m.sessionMu.Lock()
	defer m.sessionMu.Unlock()
	s, ok := m.session[req.Ref]
	if !ok {
		return nil, errors.Errorf("unknown ref %q", req.Ref)
	}
	return res, s.processes.DeleteProcess(req.ProcessID)
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
		return nil, errors.New("disconnect: empty key")
	}

	m.sessionMu.Lock()
	if s, ok := m.session[key]; ok {
		if s.cancelBuild != nil {
			s.cancelBuild()
		}
		s.cancelRunningProcesses()
		if s.result != nil {
			s.result.Done()
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
			if s.cancelBuild != nil {
				s.cancelBuild()
			}
			s.cancelRunningProcesses()
		}
	}
	m.sessionMu.Unlock()
	return nil
}

func (m *Server) Inspect(ctx context.Context, req *pb.InspectRequest) (*pb.InspectResponse, error) {
	ref := req.Ref
	if ref == "" {
		return nil, errors.New("inspect: empty key")
	}
	var bo *pb.BuildOptions
	m.sessionMu.Lock()
	if s, ok := m.session[ref]; ok {
		bo = s.buildOptions
	} else {
		m.sessionMu.Unlock()
		return nil, errors.Errorf("inspect: unknown key %v", ref)
	}
	m.sessionMu.Unlock()
	return &pb.InspectResponse{Options: bo}, nil
}

func (m *Server) Build(ctx context.Context, req *pb.BuildRequest) (*pb.BuildResponse, error) {
	ref := req.Ref
	if ref == "" {
		return nil, errors.New("build: empty key")
	}

	// Prepare status channel and session
	m.sessionMu.Lock()
	if m.session == nil {
		m.session = make(map[string]*session)
	}
	s, ok := m.session[ref]
	if ok {
		if !s.buildOnGoing.CompareAndSwap(false, true) {
			m.sessionMu.Unlock()
			return &pb.BuildResponse{}, errors.New("build ongoing")
		}
		s.cancelRunningProcesses()
		s.result = nil
	} else {
		s = &session{}
		s.buildOnGoing.Store(true)
	}

	s.processes = processes.NewManager()
	statusChan := make(chan *pb.StatusResponse)
	s.statusChan = statusChan
	inR, inW := io.Pipe()
	defer inR.Close()
	s.inputPipe = inW
	m.session[ref] = s
	m.sessionMu.Unlock()
	defer func() {
		close(statusChan)
		m.sessionMu.Lock()
		s, ok := m.session[ref]
		if ok {
			s.statusChan = nil
			s.buildOnGoing.Store(false)
		}
		m.sessionMu.Unlock()
	}()

	pw := pb.NewProgressWriter(statusChan)

	// Build the specified request
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	resp, res, buildErr := m.buildFunc(ctx, req.Options, inR, pw)
	m.sessionMu.Lock()
	if s, ok := m.session[ref]; ok {
		// NOTE: buildFunc can return *build.ResultHandle even on error (e.g. when it's implemented using (github.com/docker/buildx/controller/build).RunBuild).
		if res != nil {
			s.result = res
			s.cancelBuild = cancel
			s.buildOptions = req.Options
			m.session[ref] = s
			if buildErr != nil {
				buildErr = controllererrors.WrapBuild(buildErr, ref)
			}
		}
	} else {
		m.sessionMu.Unlock()
		return nil, errors.Errorf("build: unknown key %v", ref)
	}
	m.sessionMu.Unlock()

	if buildErr != nil {
		return nil, buildErr
	}

	if resp == nil {
		resp = &client.SolveResponse{}
	}
	return &pb.BuildResponse{
		ExporterResponse: resp.ExporterResponse,
	}, nil
}

func (m *Server) Status(req *pb.StatusRequest, stream pb.Controller_StatusServer) error {
	ref := req.Ref
	if ref == "" {
		return errors.New("status: empty key")
	}

	// Wait and get status channel prepared by Build()
	var statusChan <-chan *pb.StatusResponse
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
		if err := stream.Send(ss); err != nil {
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
		return errors.Errorf("unexpected message: %T; wanted init", msg.GetInit())
	}
	ref := init.Ref
	if ref == "" {
		return errors.New("input: no ref is provided")
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
				return errors.Wrap(ctx.Err(), "canceled")
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
	containerIn, containerOut := ioset.Pipe()
	defer func() { containerOut.Close(); containerIn.Close() }()

	initDoneCh := make(chan *processes.Process)
	initErrCh := make(chan error)
	eg, egCtx := errgroup.WithContext(context.TODO())
	srvIOCtx, srvIOCancel := context.WithCancel(egCtx)
	eg.Go(func() error {
		defer srvIOCancel()
		return serveIO(srvIOCtx, srv, func(initMessage *pb.InitMessage) (retErr error) {
			defer func() {
				if retErr != nil {
					initErrCh <- retErr
				}
			}()
			ref := initMessage.Ref
			cfg := initMessage.InvokeConfig

			m.sessionMu.Lock()
			s, ok := m.session[ref]
			if !ok {
				m.sessionMu.Unlock()
				return errors.Errorf("invoke: unknown key %v", ref)
			}
			m.sessionMu.Unlock()

			pid := initMessage.ProcessID
			if pid == "" {
				return errors.Errorf("invoke: specify process ID")
			}
			proc, ok := s.processes.Get(pid)
			if !ok {
				// Start a new process.
				if cfg == nil {
					return errors.New("no container config is provided")
				}
				var err error
				proc, err = s.processes.StartProcess(pid, s.result, cfg)
				if err != nil {
					return err
				}
			}
			// Attach containerIn to this process
			proc.ForwardIO(&containerIn, srvIOCancel)
			initDoneCh <- proc
			return nil
		}, &ioServerConfig{
			stdin:  containerOut.Stdin,
			stdout: containerOut.Stdout,
			stderr: containerOut.Stderr,
			// TODO: signal, resize
		})
	})
	eg.Go(func() (rErr error) {
		defer srvIOCancel()
		// Wait for init done
		var proc *processes.Process
		select {
		case p := <-initDoneCh:
			proc = p
		case err := <-initErrCh:
			return err
		case <-egCtx.Done():
			return egCtx.Err()
		}

		// Wait for IO done
		select {
		case <-srvIOCtx.Done():
			return srvIOCtx.Err()
		case err := <-proc.Done():
			return err
		case <-egCtx.Done():
			return egCtx.Err()
		}
	})

	return eg.Wait()
}
