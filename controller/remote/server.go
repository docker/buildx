package remote

import (
	"context"
	"io"
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
		sessions: newSessionManager(buildFunc),
	}
}

type Server struct {
	sessions *sessionManager
}

func (m *Server) isUsed() bool {
	if m.sessions.sessionCreated() && len(m.sessions.list()) == 0 {
		return false
	}
	return true
}

func (m *Server) ListProcesses(ctx context.Context, req *pb.ListProcessesRequest) (res *pb.ListProcessesResponse, err error) {
	s := m.sessions.get(req.Ref)
	if s == nil {
		return nil, errors.Errorf("unknown ref %q", req.Ref)
	}
	res = new(pb.ListProcessesResponse)
	for _, p := range s.listProcesses() {
		res.Infos = append(res.Infos, p)
	}
	return res, nil
}

func (m *Server) DisconnectProcess(ctx context.Context, req *pb.DisconnectProcessRequest) (res *pb.DisconnectProcessResponse, err error) {
	s := m.sessions.get(req.Ref)
	if s == nil {
		return nil, errors.Errorf("unknown ref %q", req.Ref)
	}
	return res, s.deleteProcess(req.ProcessID)
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
	return &pb.ListResponse{
		Keys: m.sessions.list(),
	}, nil
}

func (m *Server) Disconnect(ctx context.Context, req *pb.DisconnectRequest) (res *pb.DisconnectResponse, err error) {
	if err := m.sessions.delete(req.Ref); err != nil {
		return nil, errors.Errorf("failed to delete session %q: %v", req.Ref, err)
	}
	return &pb.DisconnectResponse{}, nil
}

func (m *Server) Close() error {
	m.sessions.close()
	return nil
}

func (m *Server) Inspect(ctx context.Context, req *pb.InspectRequest) (*pb.InspectResponse, error) {
	ref := req.Ref
	if ref == "" {
		return nil, errors.New("inspect: empty key")
	}
	s := m.sessions.get(ref)
	if s == nil {
		return nil, errors.Errorf("inspect: unknown key %v", ref)
	}
	bo := s.getBuildOptions()
	return &pb.InspectResponse{Options: bo}, nil
}

func (m *Server) Build(ctx context.Context, req *pb.BuildRequest) (*pb.BuildResponse, error) {
	ref := req.Ref
	if ref == "" {
		return nil, errors.New("build: empty key")
	}

	s := m.sessions.get(ref)
	if s == nil {
		s = m.sessions.newSession(ref)
	}
	resp, err := s.build(ctx, req.Options, func(err error) error { return controllererrors.WrapBuild(err, ref) })
	if resp == nil {
		resp = &client.SolveResponse{}
	}
	return &pb.BuildResponse{
		ExporterResponse: resp.ExporterResponse,
	}, err
}

func (m *Server) Status(req *pb.StatusRequest, stream pb.Controller_StatusServer) error {
	ref := req.Ref
	if ref == "" {
		return errors.New("status: empty key")
	}

	// Wait and get status channel prepared by Build()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var statusChan <-chan *pb.StatusResponse
	_, err := m.sessions.waitAndGet(ctx, ref, func(s *session) bool {
		statusChan = s.getStatusChan()
		return statusChan != nil
	})
	if err != nil {
		return err
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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var inputPipeW *io.PipeWriter
	if _, err := m.sessions.waitAndGet(ctx, ref, func(s *session) bool {
		inputPipeW = s.getInputWriter()
		return inputPipeW != nil
	}); err != nil {
		return err
	}

	// Forward input stream
	eg, ctx := errgroup.WithContext(context.TODO())
	doneCh := make(chan struct{})
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
			case <-doneCh:
				return nil
			case <-ctx.Done():
				return nil
			}
		}
	})
	eg.Go(func() (retErr error) {
		defer close(doneCh)
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

			s := m.sessions.get(ref)
			if s == nil {
				return errors.Errorf("invoke: unknown key %v", ref)
			}

			pid := initMessage.ProcessID
			if pid == "" {
				return errors.Errorf("invoke: specify process ID")
			}
			proc, ok := s.getProcess(pid)
			if !ok {
				// Start a new process.
				if cfg == nil {
					return errors.New("no container config is provided")
				}
				var err error
				proc, err = s.startProcess(pid, cfg)
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
