package daptest

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/docker/buildx/dap/common"
	"github.com/google/go-dap"
	"github.com/stretchr/testify/assert"
	"golang.org/x/sync/errgroup"
)

type Client struct {
	conn common.Conn

	requests   map[int]chan<- dap.ResponseMessage
	requestsMu sync.Mutex

	events   map[string][]func(dap.EventMessage)
	eventsMu sync.RWMutex

	seq    atomic.Int64
	eg     *errgroup.Group
	cancel context.CancelCauseFunc
}

func NewClient(conn common.Conn) *Client {
	c := &Client{
		conn:     conn,
		requests: make(map[int]chan<- dap.ResponseMessage),
		events:   make(map[string][]func(dap.EventMessage)),
	}

	var ctx context.Context
	ctx, c.cancel = context.WithCancelCause(context.Background())

	c.eg, _ = errgroup.WithContext(context.Background())
	c.eg.Go(func() error {
		for {
			m, err := conn.RecvMsg(ctx)
			if err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, io.EOF) {
					return nil
				}
				return err
			}

			switch m := m.(type) {
			case dap.RequestMessage:
				// TODO: no reverse requests are currently supported
				conn.SendMsg(&dap.Response{
					ProtocolMessage: dap.ProtocolMessage{
						Seq:  c.nextSeq(),
						Type: "response",
					},
					RequestSeq: m.GetRequest().GetSeq(),
					Success:    false,
					Command:    m.GetRequest().Command,
					Message:    "not implemented",
				})
			case dap.ResponseMessage:
				c.requestsMu.Lock()
				req := m.GetResponse().GetResponse().RequestSeq
				ch := c.requests[req]
				delete(c.requests, req)
				c.requestsMu.Unlock()

				if ch != nil {
					ch <- m
				}
			case dap.EventMessage:
				c.invokeEventCallback(m)
			}
		}
	})
	return c
}

func (c *Client) Do(t *testing.T, req dap.RequestMessage) <-chan dap.ResponseMessage {
	req.GetRequest().Type = "request"
	req.GetRequest().Seq = c.nextSeq()

	ch := make(chan dap.ResponseMessage, 1)

	// We need to set the channel before we send the message
	// because it's otherwise possible for us to receive the response
	// before we've registered the original request.
	c.requestsMu.Lock()
	c.requests[req.GetSeq()] = ch
	c.requestsMu.Unlock()

	if err := c.conn.SendMsg(req); err != nil {
		assert.NoError(t, err)
		close(ch)

		c.requestsMu.Lock()
		delete(c.requests, req.GetSeq())
		c.requestsMu.Unlock()
	}
	return ch
}

func DoRequest[ResponseMessage dap.ResponseMessage, RequestMessage dap.RequestMessage](t *testing.T, c *Client, req RequestMessage) <-chan ResponseMessage {
	ch := make(chan ResponseMessage, 1)
	go func() {
		defer close(ch)

		if m := <-c.Do(t, req); m != nil {
			ch <- m.(ResponseMessage)
		}
	}()
	return ch
}

func (c *Client) RegisterEvent(event string, fn func(dap.EventMessage)) {
	c.eventsMu.Lock()
	defer c.eventsMu.Unlock()

	c.events[event] = append(c.events[event], fn)
}

func (c *Client) invokeEventCallback(event dap.EventMessage) {
	c.eventsMu.RLock()
	fns := c.events[event.GetEvent().Event]
	c.eventsMu.RUnlock()

	for _, fn := range fns {
		fn(event)
	}
}

func (c *Client) Close() error {
	c.cancel(context.Canceled)
	return c.eg.Wait()
}

func (c *Client) nextSeq() int {
	seq := c.seq.Add(1)
	return int(seq)
}
