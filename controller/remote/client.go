package remote

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/containerd/console"
	"github.com/containerd/containerd/defaults"
	"github.com/containerd/containerd/pkg/dialer"
	"github.com/docker/buildx/controller/pb"
	"github.com/docker/buildx/util/progress"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/identity"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/credentials/insecure"
)

func NewClient(addr string) (*Client, error) {
	backoffConfig := backoff.DefaultConfig
	backoffConfig.MaxDelay = 3 * time.Second
	connParams := grpc.ConnectParams{
		Backoff: backoffConfig,
	}
	gopts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithConnectParams(connParams),
		grpc.WithContextDialer(dialer.ContextDialer),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(defaults.DefaultMaxRecvMsgSize)),
		grpc.WithDefaultCallOptions(grpc.MaxCallSendMsgSize(defaults.DefaultMaxSendMsgSize)),
	}
	conn, err := grpc.Dial(dialer.DialAddress(addr), gopts...)
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

func (c *Client) Disconnect(ctx context.Context, key string) error {
	_, err := c.client().Disconnect(ctx, &pb.DisconnectRequest{Ref: key})
	return err
}

func (c *Client) Invoke(ctx context.Context, ref string, containerConfig pb.ContainerConfig, in io.ReadCloser, stdout io.WriteCloser, stderr io.WriteCloser) error {
	if ref == "" {
		return errors.New("build reference must be specified")
	}
	stream, err := c.client().Invoke(ctx)
	if err != nil {
		return err
	}
	return attachIO(ctx, stream, &pb.InitMessage{Ref: ref, ContainerConfig: &containerConfig}, ioAttachConfig{
		stdin:  in,
		stdout: stdout,
		stderr: stderr,
		// TODO: Signal, Resize
	})
}

func (c *Client) Build(ctx context.Context, options pb.BuildOptions, in io.ReadCloser, w io.Writer, out console.File, progressMode string) (string, error) {
	ref := identity.NewID()
	pw, err := progress.NewPrinter(context.TODO(), w, out, progressMode)
	if err != nil {
		return "", err
	}
	statusChan := make(chan *client.SolveStatus)
	statusDone := make(chan struct{})
	eg, egCtx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		defer close(statusChan)
		return c.build(egCtx, ref, options, in, statusChan)
	})
	eg.Go(func() error {
		defer close(statusDone)
		for s := range statusChan {
			st := s
			pw.Write(st)
		}
		return nil
	})
	eg.Go(func() error {
		<-statusDone
		return pw.Wait()
	})
	return ref, eg.Wait()
}

func (c *Client) build(ctx context.Context, ref string, options pb.BuildOptions, in io.ReadCloser, statusChan chan *client.SolveStatus) error {
	eg, egCtx := errgroup.WithContext(ctx)
	done := make(chan struct{})
	eg.Go(func() error {
		defer close(done)
		if _, err := c.client().Build(egCtx, &pb.BuildRequest{
			Ref:     ref,
			Options: &options,
		}); err != nil {
			return err
		}
		return nil
	})
	eg.Go(func() error {
		stream, err := c.client().Status(egCtx, &pb.StatusRequest{
			Ref: ref,
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
			s := client.SolveStatus{}
			for _, v := range resp.Vertexes {
				s.Vertexes = append(s.Vertexes, &client.Vertex{
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
			for _, v := range resp.Statuses {
				s.Statuses = append(s.Statuses, &client.VertexStatus{
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
			for _, v := range resp.Logs {
				s.Logs = append(s.Logs, &client.VertexLog{
					Vertex:    v.Vertex,
					Stream:    int(v.Stream),
					Data:      v.Msg,
					Timestamp: v.Timestamp,
				})
			}
			for _, v := range resp.Warnings {
				s.Warnings = append(s.Warnings, &client.VertexWarning{
					Vertex:     v.Vertex,
					Level:      int(v.Level),
					Short:      v.Short,
					Detail:     v.Detail,
					URL:        v.Url,
					SourceInfo: v.Info,
					Range:      v.Ranges,
				})
			}
			statusChan <- &s
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
						Ref: ref,
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
						if stream.Send(&pb.InputMessage{
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
	return eg.Wait()
}

func (c *Client) client() pb.ControllerClient {
	return pb.NewControllerClient(c.conn)
}
