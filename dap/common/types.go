package common

import (
	"context"
	"io"

	"github.com/google/go-dap"
)

type Conn interface {
	SendMsg(m dap.Message) error
	RecvMsg(ctx context.Context) (dap.Message, error)
	io.Closer
}
