package remote

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/containerd/containerd/v2/defaults"
	"github.com/containerd/containerd/v2/pkg/dialer"
	"github.com/docker/buildx/build"
	"github.com/docker/buildx/controller/pb"
	"github.com/docker/buildx/util/progress"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/identity"
	"github.com/moby/buildkit/util/grpcerrors"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/credentials/insecure"
)

func NewClient(ctx context.Context, addr string) (*Client, error) {
	backoffConfig := backoff.DefaultConfig
	backoffConfig.MaxDelay = 3 * time.Second
	connParams := grpc.ConnectParams{
		Backoff: backoffConfig,
	}
	gopts := []grpc.DialOption{
		//nolint:staticcheck // ignore SA1019: WithBlock is deprecated and does not work with NewClient.
		grpc.WithBlock(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithConnectParams(connParams),
		grpc.WithContextDialer(dialer.ContextDialer),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(defaults.DefaultMaxRecvMsgSize)),
		grpc.WithDefaultCallOptions(grpc.MaxCallSendMsgSize(defaults.DefaultMaxSendMsgSize)),
		grpc.WithUnaryInterceptor(grpcerrors.UnaryClientInterceptor),
		grpc.WithStreamInterceptor(grpcerrors.StreamClientInterceptor),
	}
	//nolint:staticcheck // ignore SA1019: Recommended NewClient has different behavior from DialContext.
	conn, err := grpc.DialContext(ctx, dialer.DialAddress(addr), gopts...)
	if err != nil {
		return nil, err
	}
	return &Client{conn: conn}, nil
}

type Client struct {
	conn      *grpc.ClientConn
	closeOnce sync.Once
}

func (c *Client) Close() (err error) {
	c.closeOnce.Do(func() {
		err = c.conn.Close()
	})
	return
}

func (c *Client) Version(ctx context.Context) (string, string, string, error) {
	res, err := c.client().Info(ctx, &pb.InfoRequest{})
	if err != nil {
		return "", "", "", err
	}
	v := res.BuildxVersion
	return v.Package, v.Version, v.Revision, nil
}

func (c *Client) List(ctx context.Context) (keys []string, retErr error) {
	res, err := c.client().List(ctx, &pb.ListRequest{})
	if err != nil {
		return nil, err
	}
	return res.Keys, nil
}

func (c *Client) Disconnect(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return nil
	}
	_, err := c.client().Disconnect(ctx, &pb.DisconnectRequest{SessionID: sessionID})
	return err
}

func (c *Client) ListProcesses(ctx context.Context, sessionID string) (infos []*pb.ProcessInfo, retErr error) {
	res, err := c.client().ListProcesses(ctx, &pb.ListProcessesRequest{SessionID: sessionID})
	if err != nil {
		return nil, err
	}
	return res.Infos, nil
}

func (c *Client) DisconnectProcess(ctx context.Context, sessionID, pid string) error {
	_, err := c.client().DisconnectProcess(ctx, &pb.DisconnectProcessRequest{SessionID: sessionID, ProcessID: pid})
	return err
}

func (c *Client) Invoke(ctx context.Context, sessionID string, pid string, invokeConfig *pb.InvokeConfig, in io.ReadCloser, stdout io.WriteCloser, stderr io.WriteCloser) error {
	if sessionID == "" || pid == "" {
		return errors.New("build session ID must be specified")
	}
	stream, err := c.client().Invoke(ctx)
	if err != nil {
		return err
	}
	return attachIO(ctx, stream, &pb.InitMessage{SessionID: sessionID, ProcessID: pid, InvokeConfig: invokeConfig}, ioAttachConfig{
		stdin:  in,
		stdout: stdout,
		stderr: stderr,
		// TODO: Signal, Resize
	})
}

func (c *Client) Inspect(ctx context.Context, sessionID string) (*pb.InspectResponse, error) {
	return c.client().Inspect(ctx, &pb.InspectRequest{SessionID: sessionID})
}

func (c *Client) Build(ctx context.Context, options *pb.BuildOptions, in io.ReadCloser, progress progress.Writer) (string, *client.SolveResponse, *build.Inputs, error) {
	ref := identity.NewID()
	statusChan := make(chan *client.SolveStatus)
	eg, egCtx := errgroup.WithContext(ctx)
	var resp *client.SolveResponse
	eg.Go(func() error {
		defer close(statusChan)
		var err error
		resp, err = c.build(egCtx, ref, options, in, statusChan)
		return err
	})
	eg.Go(func() error {
		for s := range statusChan {
			st := s
			progress.Write(st)
		}
		return nil
	})
	return ref, resp, nil, eg.Wait()
}

func (c *Client) build(ctx context.Context, sessionID string, options *pb.BuildOptions, in io.ReadCloser, statusChan chan *client.SolveStatus) (*client.SolveResponse, error) {
	eg, egCtx := errgroup.WithContext(ctx)
	done := make(chan struct{})

	var resp *client.SolveResponse

	eg.Go(func() error {
		defer close(done)
		pbResp, err := c.client().Build(egCtx, &pb.BuildRequest{
			SessionID: sessionID,
			Options:   options,
		})
		if err != nil {
			return err
		}
		resp = &client.SolveResponse{
			ExporterResponse: pbResp.ExporterResponse,
		}
		return nil
	})
	eg.Go(func() error {
		stream, err := c.client().Status(egCtx, &pb.StatusRequest{
			SessionID: sessionID,
		})
		if err != nil {
			return err
		}
		for {
			resp, err := stream.Recv()
			if err != nil {
				if err == io.EOF {
					return nil
				}
				return errors.Wrap(err, "failed to receive status")
			}
			statusChan <- pb.FromControlStatus(resp)
		}
	})
	if in != nil {
		eg.Go(func() error {
			stream, err := c.client().Input(egCtx)
			if err != nil {
				return err
			}
			if err := stream.Send(&pb.InputMessage{
				Input: &pb.InputMessage_Init{
					Init: &pb.InputInitMessage{
						SessionID: sessionID,
					},
				},
			}); err != nil {
				return errors.Wrap(err, "failed to init input")
			}

			inReader, inWriter := io.Pipe()
			eg2, _ := errgroup.WithContext(ctx)
			eg2.Go(func() error {
				<-done
				return inWriter.Close()
			})
			go func() {
				// do not wait for read completion but return here and let the caller send EOF
				// this allows us to return on ctx.Done() without being blocked by this reader.
				io.Copy(inWriter, in)
				inWriter.Close()
			}()
			eg2.Go(func() error {
				for {
					buf := make([]byte, 32*1024)
					n, err := inReader.Read(buf)
					if err != nil {
						if err == io.EOF {
							break // break loop and send EOF
						}
						return err
					} else if n > 0 {
						if err := stream.Send(&pb.InputMessage{
							Input: &pb.InputMessage_Data{
								Data: &pb.DataMessage{
									Data: buf[:n],
								},
							},
						}); err != nil {
							return err
						}
					}
				}
				return stream.Send(&pb.InputMessage{
					Input: &pb.InputMessage_Data{
						Data: &pb.DataMessage{
							EOF: true,
						},
					},
				})
			})
			return eg2.Wait()
		})
	}
	return resp, eg.Wait()
}

func (c *Client) client() pb.ControllerClient {
	return pb.NewControllerClient(c.conn)
}
