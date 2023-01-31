package ioset

import (
	"bufio"
	"fmt"
	"io"
	"sync"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

type MuxOut struct {
	Out
	EnableHook  func()
	DisableHook func()
}

// NewMuxIO forwards IO stream to/from "in" and "outs".
// It toggles IO when it detects "C-a-c" key.
// "outs" are closed automatically when "in" reaches EOF.
// "in" doesn't closed automatically so the caller needs to explicitly close it.
func NewMuxIO(in In, outs []MuxOut, initIdx int, toggleMessage func(prev int, res int) string) *MuxIO {
	m := &MuxIO{
		enabled:       make(map[int]struct{}),
		in:            in,
		outs:          outs,
		closedCh:      make(chan struct{}),
		toggleMessage: toggleMessage,
	}
	for i := range outs {
		m.enabled[i] = struct{}{}
	}
	m.maxCur = len(outs)
	m.cur = initIdx
	var wg sync.WaitGroup
	var mu sync.Mutex
	for i, o := range outs {
		i, o := i, o
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := copyToFunc(o.Stdout, func() (io.Writer, error) {
				if m.cur == i {
					return in.Stdout, nil
				}
				return nil, nil
			}); err != nil {
				logrus.WithField("output index", i).WithError(err).Warnf("failed to write stdout")
			}
			if err := o.Stdout.Close(); err != nil {
				logrus.WithField("output index", i).WithError(err).Warnf("failed to close stdout")
			}
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := copyToFunc(o.Stderr, func() (io.Writer, error) {
				if m.cur == i {
					return in.Stderr, nil
				}
				return nil, nil
			}); err != nil {
				logrus.WithField("output index", i).WithError(err).Warnf("failed to write stderr")
			}
			if err := o.Stderr.Close(); err != nil {
				logrus.WithField("output index", i).WithError(err).Warnf("failed to close stderr")
			}
		}()
	}
	go func() {
		errToggle := errors.Errorf("toggle IO")
		for {
			prevIsControlSequence := false
			if err := copyToFunc(traceReader(in.Stdin, func(r rune) (bool, error) {
				// Toggle IO when it detects C-a-c
				// TODO: make it configurable if needed
				if int(r) == 1 {
					prevIsControlSequence = true
					return false, nil
				}
				defer func() { prevIsControlSequence = false }()
				if prevIsControlSequence {
					if string(r) == "c" {
						return false, errToggle
					}
				}
				return true, nil
			}), func() (io.Writer, error) {
				mu.Lock()
				o := outs[m.cur]
				mu.Unlock()
				return o.Stdin, nil
			}); !errors.Is(err, errToggle) {
				if err != nil {
					logrus.WithError(err).Warnf("failed to read stdin")
				}
				break
			}
			m.toggleIO()
		}

		// propagate stdin EOF
		for i, o := range outs {
			if err := o.Stdin.Close(); err != nil {
				logrus.WithError(err).Warnf("failed to close stdin of %d", i)
			}
		}
		wg.Wait()
		close(m.closedCh)
	}()
	return m
}

type MuxIO struct {
	cur           int
	maxCur        int
	enabled       map[int]struct{}
	mu            sync.Mutex
	in            In
	outs          []MuxOut
	closedCh      chan struct{}
	toggleMessage func(prev int, res int) string
}

func (m *MuxIO) waitClosed() {
	<-m.closedCh
}

func (m *MuxIO) Enable(i int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.enabled[i] = struct{}{}
}

func (m *MuxIO) Disable(i int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if i == 0 {
		return errors.Errorf("disabling 0th io is prohibited")
	}
	delete(m.enabled, i)
	if m.cur == i {
		m.toggleIO()
	}
	return nil
}

func (m *MuxIO) toggleIO() {
	if m.outs[m.cur].DisableHook != nil {
		m.outs[m.cur].DisableHook()
	}
	prev := m.cur
	for {
		if m.cur+1 >= m.maxCur {
			m.cur = 0
		} else {
			m.cur++
		}
		if _, ok := m.enabled[m.cur]; !ok {
			continue
		}
		break
	}
	res := m.cur
	if m.outs[m.cur].EnableHook != nil {
		m.outs[m.cur].EnableHook()
	}
	fmt.Fprint(m.in.Stdout, m.toggleMessage(prev, res))
}

func traceReader(r io.ReadCloser, f func(rune) (bool, error)) io.ReadCloser {
	pr, pw := io.Pipe()
	go func() {
		br := bufio.NewReader(r)
		for {
			rn, _, err := br.ReadRune()
			if err != nil {
				if err == io.EOF {
					pw.Close()
					return
				}
				pw.CloseWithError(err)
				return
			}
			if isWrite, err := f(rn); err != nil {
				pw.CloseWithError(err)
				return
			} else if !isWrite {
				continue
			}
			if _, err := pw.Write([]byte(string(rn))); err != nil {
				pw.CloseWithError(err)
				return
			}
		}
	}()
	return &readerWithClose{
		Reader: pr,
		closeFunc: func() error {
			pr.Close()
			return r.Close()
		},
	}
}

func copyToFunc(r io.Reader, wFunc func() (io.Writer, error)) error {
	buf := make([]byte, 4096)
	for {
		n, readErr := r.Read(buf)
		if readErr != nil && readErr != io.EOF {
			return readErr
		}
		w, err := wFunc()
		if err != nil {
			return err
		}
		if w != nil {
			if _, err := w.Write(buf[:n]); err != nil {
				logrus.WithError(err).Debugf("failed to copy")
			}
		}
		if readErr == io.EOF {
			return nil
		}
	}
}

type readerWithClose struct {
	io.Reader
	closeFunc func() error
}

func (r *readerWithClose) Close() error {
	return r.closeFunc()
}
