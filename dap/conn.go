package dap

import (
	"bufio"
	"context"
	"io"
	"sync"

	"github.com/docker/buildx/dap/common"
	"github.com/google/go-dap"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
)

type Conn = common.Conn

type conn struct {
	recvCh <-chan dap.Message
	sendCh chan<- dap.Message

	ctx    context.Context
	cancel context.CancelCauseFunc

	eg   *errgroup.Group
	once sync.Once
}

func NewConn(rd io.Reader, wr io.Writer) Conn {
	recvCh := make(chan dap.Message, 100)
	sendCh := make(chan dap.Message, 100)
	errCh := make(chan error, 1)

	// Reader input may never close so this is an orphaned goroutine.
	// It's ok if it does actually close but not necessary for the
	// proper functioning of this connection.
	//
	// The reason this might not close is because stdin close is controlled
	// by the OS and can't be closed from within the program.
	go func() {
		defer close(errCh)
		defer close(recvCh)

		rd := bufio.NewReader(rd)
		for {
			m, err := dap.ReadProtocolMessage(rd)
			if err != nil {
				if !errors.Is(err, io.EOF) {
					// TODO: not actually using this yet
					errCh <- err
				}
				return
			}
			recvCh <- m
		}
	}()

	eg, _ := errgroup.WithContext(context.Background())
	eg.Go(func() error {
		for m := range sendCh {
			if err := dap.WriteProtocolMessage(wr, m); err != nil {
				return err
			}
		}
		return nil
	})

	ctx, cancel := context.WithCancelCause(context.Background())
	return &conn{
		recvCh: recvCh,
		sendCh: sendCh,
		ctx:    ctx,
		cancel: cancel,
		eg:     eg,
	}
}

func (c *conn) SendMsg(m dap.Message) error {
	select {
	case c.sendCh <- m:
		return nil
	default:
		return errors.New("send channel full")
	}
}

func (c *conn) RecvMsg(ctx context.Context) (dap.Message, error) {
	select {
	case m, ok := <-c.recvCh:
		if !ok {
			return nil, io.EOF
		}
		return m, nil
	case <-ctx.Done():
		return nil, context.Cause(ctx)
	case <-c.ctx.Done():
		return nil, c.ctx.Err()
	}
}

func (c *conn) Close() error {
	c.cancel(context.Canceled)
	c.once.Do(func() {
		close(c.sendCh)
	})
	return c.eg.Wait()
}
