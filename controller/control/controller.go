package control

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/containerd/console"
	"github.com/docker/buildx/build"
	cbuild "github.com/docker/buildx/controller/build"
	controllerapi "github.com/docker/buildx/controller/pb"
	"github.com/docker/buildx/store"
	"github.com/docker/buildx/util/progress"
	controlapi "github.com/moby/buildkit/api/services/control"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/util/progress/progressui"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
)

type BuildxController interface {
	Build(ctx context.Context, options controllerapi.BuildOptions, in io.ReadCloser, statusChan chan *controllerapi.StatusResponse) (ref string, resp *client.SolveResponse, err error)
	// Invoke starts an IO session into the specified process.
	// If pid doesn't matche to any running processes, it starts a new process with the specified config.
	// If there is no container running or InvokeConfig.Rollback is speicfied, the process will start in a newly created container.
	// NOTE: If needed, in the future, we can split this API into three APIs (NewContainer, NewProcess and Attach).
	Invoke(ctx context.Context, ref, pid string, options controllerapi.InvokeConfig, ioIn io.ReadCloser, ioOut io.WriteCloser, ioErr io.WriteCloser) error
	Kill(ctx context.Context) error
	Close() error
	List(ctx context.Context) (refs []string, _ error)
	Disconnect(ctx context.Context, ref string) error
	ListProcesses(ctx context.Context, ref string) (infos []*controllerapi.ProcessInfo, retErr error)
	DisconnectProcess(ctx context.Context, ref, pid string) error
}

type ControlOptions struct {
	ServerConfig string
	Root         string
	Detach       bool
}

// Build is a helper function that builds the build and prints the status using the controller.
func Build(ctx context.Context, c BuildxController, options controllerapi.BuildOptions, in io.ReadCloser, w io.Writer, out console.File, progressMode string) (ref string, resp *client.SolveResponse, err error) {
	pwCh := make(chan *progress.Printer)
	statusChan := make(chan *controllerapi.StatusResponse)
	statusDone := make(chan struct{})
	eg, egCtx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		defer close(statusChan)
		var err error
		ref, resp, err = c.Build(egCtx, options, in, statusChan)
		return err
	})
	var resultInfo []*controllerapi.ResultInfoMessage
	eg.Go(func() error {
		defer close(statusDone)
		var pw *progress.Printer
		for s := range statusChan {
			st := s
			switch m := st.GetStatus().(type) {
			case *controllerapi.StatusResponse_NodeInfo:
				if pw != nil {
					return errors.Errorf("node info is already registered")
				}
				ng := m.NodeInfo
				pw, err = progress.NewPrinter(context.TODO(), w, out, progressMode, progressui.WithDesc(
					fmt.Sprintf("building with %q instance using %s driver", ng.Name, ng.Driver),
					fmt.Sprintf("%s:%s", ng.Driver, ng.Name),
				))
				if err != nil {
					return err
				}
				pwCh <- pw
			case *controllerapi.StatusResponse_SolveStatus:
				if pw != nil {
					pw.Write(toSolveStatus(m.SolveStatus))
				}
			case *controllerapi.StatusResponse_ResultInfo:
				resultInfo = append(resultInfo, m.ResultInfo)
			}
		}
		return nil
	})
	eg.Go(func() error {
		pw := <-pwCh
		<-statusDone
		if err := pw.Wait(); err != nil {
			return err
		}
		cbuild.PrintWarnings(w, pw.Warnings(), progressMode)
		return nil
	})
	if err := eg.Wait(); err != nil {
		return ref, resp, err
	}
	for _, ri := range resultInfo {
		cbuild.PrintResult(w, &build.PrintFunc{Name: ri.Name, Format: ri.Format}, ri.Result)
	}
	return ref, resp, nil
}

// ForwardProgress creates a progress printer backed by Status API.
func ForwardProgress(statusChan chan *controllerapi.StatusResponse) cbuild.ProgressConfig {
	return cbuild.ProgressConfig{
		Printer: func(ctx context.Context, ng *store.NodeGroup) (*progress.Printer, error) {
			statusChan <- &controllerapi.StatusResponse{
				Status: &controllerapi.StatusResponse_NodeInfo{
					NodeInfo: &controllerapi.NodeInfoMessage{Name: ng.Name, Driver: ng.Driver},
				},
			}
			pw, err := progress.NewPrinter(ctx, io.Discard, os.Stderr, "quiet")
			if err != nil {
				return nil, err
			}
			return progress.Tee(pw, forwardStatus(statusChan)), nil
		},
		PrintResultFunc: func(f *build.PrintFunc, res map[string]string) error {
			statusChan <- &controllerapi.StatusResponse{
				Status: &controllerapi.StatusResponse_ResultInfo{
					ResultInfo: &controllerapi.ResultInfoMessage{
						Result: res,
						Name:   f.Name,
						Format: f.Format,
					},
				},
			}
			return nil
		},
		// PrintWarningsFunc: printed on the client side
	}
}

func forwardStatus(statusChan chan *controllerapi.StatusResponse) chan *client.SolveStatus {
	ch := make(chan *client.SolveStatus)
	go func() {
		for st := range ch {
			st2 := toControlStatus(st)
			statusChan <- &controllerapi.StatusResponse{
				Status: &controllerapi.StatusResponse_SolveStatus{
					SolveStatus: st2,
				},
			}
		}
	}()
	return ch
}

func toControlStatus(s *client.SolveStatus) *controllerapi.SolveStatusMessage {
	resp := controllerapi.SolveStatusMessage{}
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

func toSolveStatus(resp *controllerapi.SolveStatusMessage) *client.SolveStatus {
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
	return &s
}
