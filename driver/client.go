package driver

import (
	"context"

	controlapi "github.com/moby/buildkit/api/services/control"
	"github.com/moby/buildkit/client"
	gateway "github.com/moby/buildkit/frontend/gateway/client"
)

type Client interface {
	Build(ctx context.Context, opt client.SolveOpt, product string, buildFunc gateway.BuildFunc, statusChan chan *client.SolveStatus) (*client.SolveResponse, error)
	ListWorkers(ctx context.Context, opts ...client.ListWorkersOption) ([]*client.WorkerInfo, error)
	Info(ctx context.Context) (*client.Info, error)
	DiskUsage(ctx context.Context, opts ...client.DiskUsageOption) ([]*client.UsageInfo, error)
	Prune(ctx context.Context, ch chan client.UsageInfo, opts ...client.PruneOption) error
	ControlClient() controlapi.ControlClient
	Close() error
	Wait(ctx context.Context) error
}
