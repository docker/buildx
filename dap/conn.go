package dap

import (
	"bufio"
	"context"
	"io"
	"sync"

	"github.com/google/go-dap"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
)

type Conn interface {
	SendMsg(m dap.Message) error
	RecvMsg(ctx context.Context) (dap.Message, error)
	io.Closer
}

type conn struct {
	recvCh <-chan dap.Message
	sendCh chan<- dap.Message

	ctx    context.Context
	cancel context.CancelCauseFunc
}

func Pipe() (Conn, Conn) {
	ch1 := make(chan dap.Message, 100)
	ch2 := make(chan dap.Message, 100)

	ctx, cancel := context.WithCancelCause(context.Background())
	conn1 := &conn{
		recvCh: ch1,
		sendCh: ch2,
		ctx:    ctx,
		cancel: cancel,
	}
	conn2 := &conn{
		recvCh: ch2,
		sendCh: ch1,
		ctx:    ctx,
		cancel: cancel,
	}
	return conn1, conn2
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
	case m := <-c.recvCh:
		return m, nil
	case <-ctx.Done():
		return nil, context.Cause(ctx)
	case <-c.ctx.Done():
		return nil, c.ctx.Err()
	}
}

func (c *conn) Close() error {
	c.cancel(context.Canceled)
	return nil
}

type ioConn struct {
	conn
	eg   *errgroup.Group
	once sync.Once
}

func (c *ioConn) Close() error {
	c.cancel(context.Canceled)
	c.once.Do(func() {
		close(c.sendCh)
	})
	return c.eg.Wait()
}

func IoConn(rdwr io.ReadWriter) Conn {
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

		rd := bufio.NewReader(rdwr)
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
			if err := dap.WriteProtocolMessage(rdwr, m); err != nil {
				return err
			}
		}
		return nil
	})

	ctx, cancel := context.WithCancelCause(context.Background())
	return &ioConn{
		conn: conn{
			recvCh: recvCh,
			sendCh: sendCh,
			ctx:    ctx,
			cancel: cancel,
		},
		eg: eg,
	}
}
