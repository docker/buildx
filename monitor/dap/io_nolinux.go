//go:build !linux

package dap

import (
	"context"
	"io"

	"github.com/pkg/errors"
)

func AttachContainerIO(root string, stdin io.Reader, stdout, stderr io.Writer, setTtyRaw bool) error {
	return errors.Errorf("unsupported")
}

func serveContainerIO(ctx context.Context, root string) (io.ReadCloser, io.WriteCloser, io.WriteCloser, func(), error) {
	return nil, nil, nil, nil, errors.Errorf("unsupported")
}
