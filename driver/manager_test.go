package driver

import (
	"context"
	"testing"

	"github.com/moby/buildkit/client"
	"github.com/stretchr/testify/require"
)

func TestBootRetriesClientAfterErrNotRunning(t *testing.T) {
	d := &retryDriver{client: &client.Client{}}

	c, err := Boot(context.Background(), context.Background(), &DriverHandle{Driver: d}, nil)
	require.NoError(t, err)
	require.Same(t, d.client, c)
	require.Equal(t, 2, d.clientCalls)
}

type retryDriver struct {
	Driver
	client      *client.Client
	clientCalls int
}

func (d *retryDriver) Info(context.Context) (*Info, error) {
	return &Info{Status: Running}, nil
}

func (d *retryDriver) Client(context.Context, ...client.ClientOpt) (*client.Client, error) {
	d.clientCalls++
	if d.clientCalls == 1 {
		return nil, ErrNotRunning{}
	}
	return d.client, nil
}
