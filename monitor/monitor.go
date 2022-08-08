package monitor

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/docker/buildx/build"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"golang.org/x/term"
)

const helpMessage = `
Available commads are:
  reload   reloads the context and build it.
  rollback re-runs the interactive container with initial rootfs contents.
  exit     exits monitor.
  help     shows this message.
`

// RunMonitor provides an interactive session for running and managing containers via specified IO.
func RunMonitor(ctx context.Context, containerConfig build.ContainerConfig, reloadFunc func(context.Context) (*build.ResultContext, error), stdin io.ReadCloser, stdout, stderr io.WriteCloser) error {
	monitorIn, monitorOut := ioSetPipe()
	defer monitorIn.Close()
	monitorEnableCh := make(chan struct{})
	monitorDisableCh := make(chan struct{})
	monitorOutCtx := ioSetOutContext{monitorOut,
		func() { monitorEnableCh <- struct{}{} },
		func() { monitorDisableCh <- struct{}{} },
	}

	containerIn, containerOut := ioSetPipe()
	defer containerIn.Close()
	containerOutCtx := ioSetOutContext{containerOut,
		// send newline to hopefully get the prompt; TODO: better UI (e.g. reprinting the last line)
		func() { containerOut.stdin.Write([]byte("\n")) },
		func() {},
	}

	m := &monitor{
		invokeIO: newIOForwarder(containerIn),
		muxIO: newMuxIO(ioSetIn{stdin, stdout, stderr}, []ioSetOutContext{monitorOutCtx, containerOutCtx}, 1, func(prev int, res int) string {
			if prev == 0 && res == 0 {
				// No toggle happened because container I/O isn't enabled.
				return "No running interactive containers. You can start one by issuing rollback command\n"
			}
			return "Switched IO\n"
		}),
	}

	// Start container automatically
	go func() {
		fmt.Fprintf(stdout, "Launching interactive container. Press Ctrl-a-c to switch to monitor console\n")
		m.rollback(ctx, containerConfig)
	}()

	// Serve monitor commands
	monitorForwarder := newIOForwarder(monitorIn)
	for {
		<-monitorEnableCh
		in, out := ioSetPipe()
		monitorForwarder.setDestination(&out)
		doneCh, errCh := make(chan struct{}), make(chan error)
		go func() {
			defer close(doneCh)
			defer in.Close()
			t := term.NewTerminal(readWriter{in.stdin, in.stdout}, "(buildx) ")
			for {
				l, err := t.ReadLine()
				if err != nil {
					if err != io.EOF {
						errCh <- err
						return
					}
					return
				}
				switch l {
				case "":
					// nop
				case "reload":
					res, err := reloadFunc(ctx)
					if err != nil {
						fmt.Printf("failed to reload: %v\n", err)
					} else {
						// rollback the running container with the new result
						containerConfig.ResultCtx = res
						m.rollback(ctx, containerConfig)
						fmt.Fprint(stdout, "Interactive container was restarted. Press Ctrl-a-c to switch to the new container\n")
					}
				case "rollback":
					m.rollback(ctx, containerConfig)
					fmt.Fprint(stdout, "Interactive container was restarted. Press Ctrl-a-c to switch to the new container\n")
				case "exit":
					return
				case "help":
					fmt.Fprint(stdout, helpMessage)
				default:
					fmt.Printf("unknown command: %q\n", l)
					fmt.Fprint(stdout, helpMessage)
				}
			}
		}()
		select {
		case <-doneCh:
			return nil
		case err := <-errCh:
			return err
		case <-monitorDisableCh:
		}
		monitorForwarder.setDestination(nil)
	}
}

type readWriter struct {
	io.Reader
	io.Writer
}

type monitor struct {
	muxIO           *muxIO
	invokeIO        *ioForwarder
	curInvokeCancel func()
}

func (m *monitor) rollback(ctx context.Context, cfg build.ContainerConfig) {
	if m.curInvokeCancel != nil {
		m.curInvokeCancel() // Finish the running container if exists
	}
	go func() {
		// Start a new container
		if err := m.invoke(ctx, cfg); err != nil {
			logrus.Debugf("invoke error: %v", err)
		}
	}()
}

func (m *monitor) invoke(ctx context.Context, cfg build.ContainerConfig) error {
	m.muxIO.enable(1)
	defer m.muxIO.disable(1)
	invokeCtx, invokeCancel := context.WithCancel(ctx)

	containerIn, containerOut := ioSetPipe()
	m.invokeIO.setDestination(&containerOut)
	waitInvokeDoneCh := make(chan struct{})
	var cancelOnce sync.Once
	curInvokeCancel := func() {
		cancelOnce.Do(func() {
			containerIn.Close()
			m.invokeIO.setDestination(nil)
			invokeCancel()
		})
		<-waitInvokeDoneCh
	}
	defer curInvokeCancel()
	m.curInvokeCancel = curInvokeCancel

	cfg.Stdin = containerIn.stdin
	cfg.Stdout = containerIn.stdout
	cfg.Stderr = containerIn.stderr
	err := build.Invoke(invokeCtx, cfg)
	close(waitInvokeDoneCh)

	return err
}

type ioForwarder struct {
	curIO    *ioSetOut
	mu       sync.Mutex
	updateCh chan struct{}
}

func newIOForwarder(in ioSetIn) *ioForwarder {
	f := &ioForwarder{
		updateCh: make(chan struct{}),
	}
	doneCh := make(chan struct{})
	go func() {
		for {
			f.mu.Lock()
			w := f.curIO
			f.mu.Unlock()
			if w != nil && w.stdout != nil && w.stderr != nil {
				go func() {
					if _, err := io.Copy(in.stdout, w.stdout); err != nil && err != io.ErrClosedPipe {
						// ErrClosedPipe is OK as we close this read end during setDestination.
						logrus.WithError(err).Warnf("failed to forward stdout: %v", err)
					}
				}()
				go func() {
					if _, err := io.Copy(in.stderr, w.stderr); err != nil && err != io.ErrClosedPipe {
						// ErrClosedPipe is OK as we close this read end during setDestination.
						logrus.WithError(err).Warnf("failed to forward stderr: %v", err)
					}
				}()
			}
			select {
			case <-f.updateCh:
			case <-doneCh:
				return
			}
		}
	}()
	go func() {
		if err := copyToFunc(in.stdin, func() (io.Writer, error) {
			f.mu.Lock()
			w := f.curIO
			f.mu.Unlock()
			if w != nil {
				return w.stdin, nil
			}
			return nil, nil
		}); err != nil && err != io.ErrClosedPipe {
			logrus.WithError(err).Warnf("failed to forward IO: %v", err)
		}
		close(doneCh)

		if w := f.curIO; w != nil {
			// Propagate close
			if err := w.Close(); err != nil {
				logrus.WithError(err).Warnf("failed to forwarded stdin IO: %v", err)
			}
		}
	}()
	return f
}

func (f *ioForwarder) setDestination(out *ioSetOut) {
	f.mu.Lock()
	if f.curIO != nil {
		// close all stream on the current IO no to mix with the new IO
		f.curIO.Close()
	}
	f.curIO = out
	f.mu.Unlock()
	f.updateCh <- struct{}{}
}

type ioSetOutContext struct {
	ioSetOut
	enableHook  func()
	disableHook func()
}

// newMuxIO forwards IO stream to/from "in" and "outs".
// "outs" are closed automatically when "in" reaches EOF.
// "in" doesn't closed automatically so the caller needs to explicitly close it.
func newMuxIO(in ioSetIn, out []ioSetOutContext, initIdx int, toggleMessage func(prev int, res int) string) *muxIO {
	m := &muxIO{
		enabled:       make(map[int]struct{}),
		in:            in,
		out:           out,
		closedCh:      make(chan struct{}),
		toggleMessage: toggleMessage,
	}
	for i := range out {
		m.enabled[i] = struct{}{}
	}
	m.maxCur = len(out)
	m.cur = initIdx
	var wg sync.WaitGroup
	var mu sync.Mutex
	for i, o := range out {
		i, o := i, o
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := copyToFunc(o.stdout, func() (io.Writer, error) {
				if m.cur == i {
					return in.stdout, nil
				}
				return nil, nil
			}); err != nil {
				logrus.WithField("output index", i).WithError(err).Warnf("failed to write stdout")
			}
			if err := o.stdout.Close(); err != nil {
				logrus.WithField("output index", i).WithError(err).Warnf("failed to close stdout")
			}
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := copyToFunc(o.stderr, func() (io.Writer, error) {
				if m.cur == i {
					return in.stderr, nil
				}
				return nil, nil
			}); err != nil {
				logrus.WithField("output index", i).WithError(err).Warnf("failed to write stderr")
			}
			if err := o.stderr.Close(); err != nil {
				logrus.WithField("output index", i).WithError(err).Warnf("failed to close stderr")
			}
		}()
	}
	go func() {
		errToggle := errors.Errorf("toggle IO")
		for {
			prevIsControlSequence := false
			if err := copyToFunc(traceReader(in.stdin, func(r rune) (bool, error) {
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
				o := out[m.cur]
				mu.Unlock()
				return o.stdin, nil
			}); !errors.Is(err, errToggle) {
				if err != nil {
					logrus.WithError(err).Warnf("failed to read stdin")
				}
				break
			}
			m.toggleIO()
		}

		// propagate stdin EOF
		for i, o := range out {
			if err := o.stdin.Close(); err != nil {
				logrus.WithError(err).Warnf("failed to close stdin of %d", i)
			}
		}
		wg.Wait()
		close(m.closedCh)
	}()
	return m
}

type muxIO struct {
	cur           int
	maxCur        int
	enabled       map[int]struct{}
	mu            sync.Mutex
	in            ioSetIn
	out           []ioSetOutContext
	closedCh      chan struct{}
	toggleMessage func(prev int, res int) string
}

func (m *muxIO) waitClosed() {
	<-m.closedCh
}

func (m *muxIO) enable(i int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.enabled[i] = struct{}{}
}

func (m *muxIO) disable(i int) error {
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

func (m *muxIO) toggleIO() {
	if m.out[m.cur].disableHook != nil {
		m.out[m.cur].disableHook()
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
	if m.out[m.cur].enableHook != nil {
		m.out[m.cur].enableHook()
	}
	fmt.Fprint(m.in.stdout, m.toggleMessage(prev, res))
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

func ioSetPipe() (ioSetIn, ioSetOut) {
	r1, w1 := io.Pipe()
	r2, w2 := io.Pipe()
	r3, w3 := io.Pipe()
	return ioSetIn{r1, w2, w3}, ioSetOut{w1, r2, r3}
}

type ioSetIn struct {
	stdin  io.ReadCloser
	stdout io.WriteCloser
	stderr io.WriteCloser
}

func (s ioSetIn) Close() (retErr error) {
	if err := s.stdin.Close(); err != nil {
		retErr = err
	}
	if err := s.stdout.Close(); err != nil {
		retErr = err
	}
	if err := s.stderr.Close(); err != nil {
		retErr = err
	}
	return
}

type ioSetOut struct {
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser
}

func (s ioSetOut) Close() (retErr error) {
	if err := s.stdin.Close(); err != nil {
		retErr = err
	}
	if err := s.stdout.Close(); err != nil {
		retErr = err
	}
	if err := s.stderr.Close(); err != nil {
		retErr = err
	}
	return
}

type readerWithClose struct {
	io.Reader
	closeFunc func() error
}

func (r *readerWithClose) Close() error {
	return r.closeFunc()
}
