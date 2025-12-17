package daptest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"

	"github.com/docker/buildx/dap/common"
	"github.com/google/go-dap"
)

func LogConn(t *testing.T, prefix string, conn common.Conn) common.Conn {
	return &loggingConn{
		Conn:   conn,
		t:      t,
		prefix: prefix,
	}
}

type loggingConn struct {
	common.Conn
	t      *testing.T
	prefix string

	outBuf []byte
}

func (c *loggingConn) SendMsg(m dap.Message) error {
	c.t.Helper()

	b, _ := json.Marshal(m)
	c.t.Logf("[%s] send: %v", c.prefix, string(b))

	err := c.Conn.SendMsg(m)
	if err != nil {
		c.t.Logf("[%s] send error: %v", c.prefix, err)
	}
	return err
}

func (c *loggingConn) RecvMsg(ctx context.Context) (dap.Message, error) {
	c.t.Helper()

	m, err := c.Conn.RecvMsg(ctx)
	if err != nil {
		if !errors.Is(err, context.Canceled) && !errors.Is(err, io.EOF) {
			c.t.Logf("[%s] recv error: %v", c.prefix, err)
		}
		return nil, err
	}

	if e, ok := m.(dap.EventMessage); ok {
		if drop := c.handleEvent(e); drop {
			return m, nil
		}
	}

	b, _ := json.Marshal(m)
	c.t.Logf("[%s] recv: %v", c.prefix, string(b))
	return m, nil
}

func (c *loggingConn) handleEvent(e dap.EventMessage) bool {
	switch e.GetEvent().Event {
	case "output":
		m := e.(*dap.OutputEvent)
		c.outBuf = append(c.outBuf, []byte(m.Body.Output)...)

		for len(c.outBuf) > 0 {
			i := bytes.IndexRune(c.outBuf, '\n')
			if i < 0 {
				break
			}

			c.t.Log(string(c.outBuf[:i]))
			c.outBuf = c.outBuf[i+1:]
		}
		return true
	case "terminated":
		if len(c.outBuf) > 0 {
			c.t.Log(string(c.outBuf))
			c.outBuf = nil
		}
		return false
	default:
		return false
	}
}
