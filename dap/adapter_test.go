package dap

import (
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/docker/buildx/dap/common"
	"github.com/google/go-dap"
	"github.com/stretchr/testify/assert"
	"golang.org/x/sync/errgroup"
)

func TestLaunch(t *testing.T) {
	t.Skip("test fails with errgroup v0.16.0, that doesn't swallow panic in goroutine")

	adapter, conn, client := NewTestAdapter[common.Config](t)

	ctx, cancel := context.WithTimeoutCause(context.Background(), 10*time.Second, context.DeadlineExceeded)
	defer cancel()

	eg, _ := errgroup.WithContext(ctx)
	eg.Go(func() error {
		_, err := adapter.Start(ctx, conn)
		assert.NoError(t, err)
		return nil
	})

	var (
		initialized       = make(chan struct{})
		configurationDone <-chan *dap.ConfigurationDoneResponse
	)

	client.RegisterEvent("initialized", func(em dap.EventMessage) {
		// Send configuration done since we don't do any configuration.
		configurationDone = DoRequest[*dap.ConfigurationDoneResponse](t, client, &dap.ConfigurationDoneRequest{
			Request: dap.Request{Command: "configurationDone"},
		})
		close(initialized)
	})

	eg.Go(func() error {
		initializeResp := <-DoRequest[*dap.InitializeResponse](t, client, &dap.InitializeRequest{
			Request: dap.Request{Command: "initialize"},
		})
		assert.True(t, initializeResp.Success)
		assert.True(t, initializeResp.Body.SupportsConfigurationDoneRequest)

		launchResp := <-DoRequest[*dap.LaunchResponse](t, client, &dap.LaunchRequest{
			Request: dap.Request{Command: "launch"},
		})
		assert.True(t, launchResp.Success)

		// We should have received the initialized event.
		select {
		case <-initialized:
		default:
			assert.Fail(t, "did not receive initialized event")
		}

		select {
		case <-configurationDone:
		case <-time.After(10 * time.Second):
			assert.Fail(t, "did not receive configurationDone response")
		}
		return nil
	})

	eg.Wait()
}

func NewTestAdapter[C LaunchConfig](t *testing.T) (*Adapter[C], Conn, *Client) {
	t.Helper()

	rd1, wr1 := io.Pipe()
	rd2, wr2 := io.Pipe()

	srvConn := logConn(t, "server", NewConn(rd1, wr2))
	t.Cleanup(func() {
		srvConn.Close()
	})

	clientConn := logConn(t, "client", NewConn(rd2, wr1))
	t.Cleanup(func() {
		clientConn.Close()
	})

	adapter := New[C]()
	t.Cleanup(func() { adapter.Stop() })

	client := NewClient(clientConn)
	return adapter, srvConn, client
}

func logConn(t *testing.T, prefix string, conn Conn) Conn {
	return &loggingConn{
		Conn:   conn,
		t:      t,
		prefix: prefix,
	}
}

type loggingConn struct {
	Conn
	t      *testing.T
	prefix string
}

func (c *loggingConn) SendMsg(m dap.Message) error {
	b, _ := json.Marshal(m)
	c.t.Logf("[%s] send: %v", c.prefix, string(b))

	err := c.Conn.SendMsg(m)
	if err != nil {
		c.t.Logf("[%s] send error: %v", c.prefix, err)
	}
	return err
}

func (c *loggingConn) RecvMsg(ctx context.Context) (dap.Message, error) {
	m, err := c.Conn.RecvMsg(ctx)
	if err != nil {
		c.t.Logf("[%s] recv error: %v", c.prefix, err)
		return nil, err
	}

	b, _ := json.Marshal(m)
	c.t.Logf("[%s] recv: %v", c.prefix, string(b))
	return m, nil
}
