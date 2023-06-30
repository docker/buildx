//go:build linux

package dap

// Ported from https://github.com/ktock/buildg/blob/v0.4.1/pkg/dap/dap.go
// Copyright The buildg Authors.
// Licensed under the Apache License, Version 2.0

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"syscall"
	"time"

	"github.com/containerd/console"
	"github.com/containerd/fifo"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
)

func AttachContainerIO(root string, stdin io.Reader, stdout, stderr io.Writer, setTtyRaw bool) error {
	if root == "" {
		return errors.Errorf("root needs to be specified")
	}

	type ioSet struct {
		stdin  io.WriteCloser
		stdout io.ReadCloser
		stderr io.ReadCloser
	}
	ioSetCh := make(chan ioSet)
	errCh := make(chan error)
	go func() {
		stdin, stdout, stderr, err := openFifosClient(context.TODO(), root)
		if err != nil {
			errCh <- err
			return
		}
		ioSetCh <- ioSet{stdin, stdout, stderr}
	}()
	var (
		pStdin  io.WriteCloser
		pStdout io.ReadCloser
		pStderr io.ReadCloser
	)
	select {
	case ioSet := <-ioSetCh:
		pStdin, pStdout, pStderr = ioSet.stdin, ioSet.stdout, ioSet.stderr
	case err := <-errCh:
		return err
	case <-time.After(3 * time.Second):
		return errors.Errorf("i/o timeout; check server is up and running")
	}
	defer func() { pStdin.Close(); pStdout.Close(); pStderr.Close() }()

	if setTtyRaw {
		con := console.Current()
		if err := con.SetRaw(); err != nil {
			return errors.Errorf("failed to configure terminal: %v", err)
		}
		defer con.Reset()
	}

	go io.Copy(pStdin, stdin)
	eg, _ := errgroup.WithContext(context.TODO())
	eg.Go(func() error { _, err := io.Copy(stdout, pStdout); return err })
	eg.Go(func() error { _, err := io.Copy(stderr, pStderr); return err })
	if err := eg.Wait(); err != nil {
		return err
	}
	fmt.Fprintf(stderr, "exec finished\n")
	return nil
}

func serveContainerIO(ctx context.Context, root string) (io.ReadCloser, io.WriteCloser, io.WriteCloser, func(), error) {
	stdin, stdout, stderr, err := openFifosServer(ctx, root)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return stdin, stdout, stderr, func() {
		stdin.Close()
		stdout.Close()
		stderr.Close()
	}, nil
}

func openFifosClient(ctx context.Context, fifosDir string) (stdin io.WriteCloser, stdout, stderr io.ReadCloser, retErr error) {
	if stdin, retErr = fifo.OpenFifo(ctx, filepath.Join(fifosDir, "stdin"), syscall.O_WRONLY, 0700); retErr != nil {
		return nil, nil, nil, errors.Errorf("failed to open stdin fifo: %v", retErr)
	}
	defer func() {
		if retErr != nil && stdin != nil {
			stdin.Close()
		}
	}()
	if stdout, retErr = fifo.OpenFifo(ctx, filepath.Join(fifosDir, "stdout"), syscall.O_RDONLY, 0700); retErr != nil {
		return nil, nil, nil, errors.Errorf("failed to open stdout fifo: %v", retErr)
	}
	defer func() {
		if retErr != nil && stdout != nil {
			stdout.Close()
		}
	}()
	if stderr, retErr = fifo.OpenFifo(ctx, filepath.Join(fifosDir, "stderr"), syscall.O_RDONLY, 0700); retErr != nil {
		return nil, nil, nil, errors.Errorf("failed to open stderr fifo: %v", retErr)
	}
	defer func() {
		if retErr != nil && stderr != nil {
			stderr.Close()
		}
	}()
	return stdin, stdout, stderr, nil
}

func openFifosServer(ctx context.Context, fifosDir string) (stdin io.ReadCloser, stdout, stderr io.WriteCloser, retErr error) {
	if stdin, retErr = fifo.OpenFifo(ctx, filepath.Join(fifosDir, "stdin"), syscall.O_RDONLY|syscall.O_CREAT|syscall.O_NONBLOCK, 0700); retErr != nil {
		return nil, nil, nil, errors.Errorf("failed to open stdin fifo: %v", retErr)
	}
	defer func() {
		if retErr != nil && stdin != nil {
			stdin.Close()
		}
	}()
	if stdout, retErr = fifo.OpenFifo(ctx, filepath.Join(fifosDir, "stdout"), syscall.O_WRONLY|syscall.O_CREAT|syscall.O_NONBLOCK, 0700); retErr != nil {
		return nil, nil, nil, errors.Errorf("failed to open stdout fifo: %v", retErr)
	}
	defer func() {
		if retErr != nil && stdout != nil {
			stdout.Close()
		}
	}()
	if stderr, retErr = fifo.OpenFifo(ctx, filepath.Join(fifosDir, "stderr"), syscall.O_WRONLY|syscall.O_CREAT|syscall.O_NONBLOCK, 0700); retErr != nil {
		return nil, nil, nil, errors.Errorf("failed to open stderr fifo: %v", retErr)
	}
	defer func() {
		if retErr != nil && stderr != nil {
			stderr.Close()
		}
	}()
	return stdin, stdout, stderr, nil
}
