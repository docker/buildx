package ioset

import (
	"io"
	"sync"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// Pipe returns a pair of piped readers and writers collection.
// They are useful for controlling stdio stream using Forwarder function.
func Pipe() (In, Out) {
	r1, w1 := io.Pipe()
	r2, w2 := io.Pipe()
	r3, w3 := io.Pipe()
	return In{r1, w2, w3}, Out{w1, r2, r3}
}

type In struct {
	Stdin  io.ReadCloser
	Stdout io.WriteCloser
	Stderr io.WriteCloser
}

func (s In) Close() (retErr error) {
	if err := s.Stdin.Close(); err != nil {
		retErr = err
	}
	if err := s.Stdout.Close(); err != nil {
		retErr = err
	}
	if err := s.Stderr.Close(); err != nil {
		retErr = err
	}
	return
}

type Out struct {
	Stdin  io.WriteCloser
	Stdout io.ReadCloser
	Stderr io.ReadCloser
}

func (s Out) Close() (retErr error) {
	if err := s.Stdin.Close(); err != nil {
		retErr = err
	}
	if err := s.Stdout.Close(); err != nil {
		retErr = err
	}
	if err := s.Stderr.Close(); err != nil {
		retErr = err
	}
	return
}

// Forwarder forwards IO between readers and writers contained
// in In and Out structs.
// In and Out can be changed during forwarding using SetIn and SetOut methods.
type Forwarder struct {
	stdin  *SingleForwarder
	stdout *SingleForwarder
	stderr *SingleForwarder
	mu     sync.Mutex

	// PropagateStdinClose indicates whether EOF from Stdin of Out should be propagated.
	// If this is true, EOF from Stdin (reader) of Out closes Stdin (writer) of In.
	PropagateStdinClose bool
}

func NewForwarder() *Forwarder {
	return &Forwarder{
		stdin:               NewSingleForwarder(),
		stdout:              NewSingleForwarder(),
		stderr:              NewSingleForwarder(),
		PropagateStdinClose: true,
	}
}

func (f *Forwarder) Close() (retErr error) {
	if err := f.stdin.Close(); err != nil {
		retErr = err
	}
	if err := f.stdout.Close(); err != nil {
		retErr = err
	}
	if err := f.stderr.Close(); err != nil {
		retErr = err
	}
	return retErr
}

func (f *Forwarder) SetOut(out *Out) {
	f.mu.Lock()
	if out == nil {
		f.stdin.SetWriter(nil, func() io.WriteCloser { return nil })
		f.stdout.SetReader(nil)
		f.stderr.SetReader(nil)
	} else {
		f.stdin.SetWriter(out.Stdin, func() io.WriteCloser {
			if f.PropagateStdinClose {
				out.Stdin.Close() // propagate EOF
				logrus.Debug("forwarder: propagating stdin close")
				return nil
			}
			return out.Stdin
		})
		f.stdout.SetReader(out.Stdout)
		f.stderr.SetReader(out.Stderr)
	}
	f.mu.Unlock()
}

func (f *Forwarder) SetIn(in *In) {
	f.mu.Lock()
	if in == nil {
		f.stdin.SetReader(nil)
		f.stdout.SetWriter(nil, func() io.WriteCloser { return nil })
		f.stderr.SetWriter(nil, func() io.WriteCloser { return nil })
	} else {
		f.stdin.SetReader(in.Stdin)
		f.stdout.SetWriter(in.Stdout, func() io.WriteCloser {
			return in.Stdout // continue write; TODO: make it configurable if needed
		})
		f.stderr.SetWriter(in.Stderr, func() io.WriteCloser {
			return in.Stderr // continue write; TODO: make it configurable if needed
		})
	}
	f.mu.Unlock()
}

// SingleForwarder forwards IO from a reader to a writer.
// The reader and writer can be changed during forwarding
// using SetReader and SetWriter methods.
type SingleForwarder struct {
	curR           io.ReadCloser // closed when set another reader
	curRMu         sync.Mutex
	curW           io.WriteCloser // closed when set another writer
	curWEOFHandler func() io.WriteCloser
	curWMu         sync.Mutex

	updateRCh chan io.ReadCloser
	doneCh    chan struct{}

	closeOnce sync.Once
}

func NewSingleForwarder() *SingleForwarder {
	f := &SingleForwarder{
		updateRCh: make(chan io.ReadCloser),
		doneCh:    make(chan struct{}),
	}
	go f.doForward()
	return f
}

func (f *SingleForwarder) doForward() {
	var r io.ReadCloser
	for {
		readerInvalid := false
		if r != nil {
			go func() {
				buf := make([]byte, 4096)
				for {
					n, readErr := r.Read(buf)
					if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, io.ErrClosedPipe) {
						logrus.Debugf("single forwarder: reader error: %v", readErr)
						return
					}
					f.curWMu.Lock()
					w := f.curW
					f.curWMu.Unlock()
					if w != nil {
						if _, err := w.Write(buf[:n]); err != nil && !errors.Is(err, io.ErrClosedPipe) {
							logrus.Debugf("single forwarder: writer error: %v", err)
						}
					}
					if readerInvalid {
						return
					}
					if readErr != io.EOF {
						continue
					}

					f.curWMu.Lock()
					var newW io.WriteCloser
					if f.curWEOFHandler != nil {
						newW = f.curWEOFHandler()
					}
					f.curW = newW
					f.curWMu.Unlock()
					return
				}
			}()
		}
		select {
		case newR := <-f.updateRCh:
			f.curRMu.Lock()
			if f.curR != nil {
				f.curR.Close()
			}
			f.curR = newR
			r = newR
			readerInvalid = true
			f.curRMu.Unlock()
		case <-f.doneCh:
			return
		}
	}
}

// Close closes the both of registered reader and writer and finishes the forwarder.
func (f *SingleForwarder) Close() (retErr error) {
	f.closeOnce.Do(func() {
		f.curRMu.Lock()
		r := f.curR
		f.curR = nil
		f.curRMu.Unlock()
		if r != nil {
			if err := r.Close(); err != nil {
				retErr = err
			}
		}
		// TODO: Wait until read data fully written to the current writer if needed.
		f.curWMu.Lock()
		w := f.curW
		f.curW = nil
		f.curWMu.Unlock()
		if w != nil {
			if err := w.Close(); err != nil {
				retErr = err
			}
		}
		close(f.doneCh)
	})
	return retErr
}

// SetWriter sets the specified writer as the forward destination.
// If curWEOFHandler isn't nil, this will be called when the current reader returns EOF.
func (f *SingleForwarder) SetWriter(w io.WriteCloser, curWEOFHandler func() io.WriteCloser) {
	f.curWMu.Lock()
	if f.curW != nil {
		// close all stream on the current IO no to mix with the new IO
		f.curW.Close()
	}
	f.curW = w
	f.curWEOFHandler = curWEOFHandler
	f.curWMu.Unlock()
}

// SetWriter sets the specified reader as the forward source.
func (f *SingleForwarder) SetReader(r io.ReadCloser) {
	f.updateRCh <- r
}
