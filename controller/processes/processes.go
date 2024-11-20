package processes

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/docker/buildx/build"
	"github.com/docker/buildx/controller/pb"
	"github.com/docker/buildx/util/ioset"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// Process provides methods to control a process.
type Process struct {
	inEnd         *ioset.Forwarder
	invokeConfig  *pb.InvokeConfig
	errCh         chan error
	processCancel func()
	serveIOCancel func(error)
}

// ForwardIO forwards process's io to the specified reader/writer.
// Optionally specify ioCancelCallback which will be called when
// the process closes the specified IO. This will be useful for additional cleanup.
func (p *Process) ForwardIO(in *ioset.In, ioCancelCallback func(error)) {
	p.inEnd.SetIn(in)
	if f := p.serveIOCancel; f != nil {
		f(errors.WithStack(context.Canceled))
	}
	p.serveIOCancel = ioCancelCallback
}

// Done returns a channel where error or nil will be sent
// when the process exits.
// TODO: change this to Wait()
func (p *Process) Done() <-chan error {
	return p.errCh
}

// Manager manages a set of proceses.
type Manager struct {
	container atomic.Value
	processes sync.Map
}

// NewManager creates and returns a Manager.
func NewManager() *Manager {
	return &Manager{}
}

// Get returns the specified process.
func (m *Manager) Get(id string) (*Process, bool) {
	v, ok := m.processes.Load(id)
	if !ok {
		return nil, false
	}
	return v.(*Process), true
}

// CancelRunningProcesses cancels execution of all running processes.
func (m *Manager) CancelRunningProcesses() {
	var funcs []func()
	m.processes.Range(func(key, value any) bool {
		funcs = append(funcs, value.(*Process).processCancel)
		m.processes.Delete(key)
		return true
	})
	for _, f := range funcs {
		f()
	}
}

// ListProcesses lists all running processes.
func (m *Manager) ListProcesses() (res []*pb.ProcessInfo) {
	m.processes.Range(func(key, value any) bool {
		res = append(res, &pb.ProcessInfo{
			ProcessID:    key.(string),
			InvokeConfig: value.(*Process).invokeConfig,
		})
		return true
	})
	return res
}

// DeleteProcess deletes the specified process.
func (m *Manager) DeleteProcess(id string) error {
	p, ok := m.processes.LoadAndDelete(id)
	if !ok {
		return errors.Errorf("unknown process %q", id)
	}
	p.(*Process).processCancel()
	return nil
}

// StartProcess starts a process in the container.
// When a container isn't available (i.e. first time invoking or the container has exited) or cfg.Rollback is set,
// this method will start a new container and run the process in it. Otherwise, this method starts a new process in the
// existing container.
func (m *Manager) StartProcess(pid string, resultCtx *build.ResultHandle, cfg *pb.InvokeConfig) (*Process, error) {
	// Get the target result to invoke a container from
	var ctr *build.Container
	if a := m.container.Load(); a != nil {
		ctr = a.(*build.Container)
	}
	if cfg.Rollback || ctr == nil || ctr.IsUnavailable() {
		go m.CancelRunningProcesses()
		// (Re)create a new container if this is rollback or first time to invoke a process.
		if ctr != nil {
			go ctr.Cancel() // Finish the existing container
		}
		var err error
		ctr, err = build.NewContainer(context.TODO(), resultCtx, cfg)
		if err != nil {
			return nil, errors.Errorf("failed to create container %v", err)
		}
		m.container.Store(ctr)
	}
	// [client(ForwardIO)] <-forwarder(switchable)-> [out] <-pipe-> [in] <- [process]
	in, out := ioset.Pipe()
	f := ioset.NewForwarder()
	f.PropagateStdinClose = false
	f.SetOut(&out)

	// Register process
	ctx, cancel := context.WithCancelCause(context.TODO())
	var cancelOnce sync.Once
	processCancelFunc := func() {
		cancelOnce.Do(func() {
			cancel(errors.WithStack(context.Canceled))
			f.Close()
			in.Close()
			out.Close()
		})
	}
	p := &Process{
		inEnd:         f,
		invokeConfig:  cfg,
		processCancel: processCancelFunc,
		errCh:         make(chan error),
	}
	m.processes.Store(pid, p)
	go func() {
		var err error
		if err = ctr.Exec(ctx, cfg, in.Stdin, in.Stdout, in.Stderr); err != nil {
			logrus.Debugf("process error: %v", err)
		}
		logrus.Debugf("finished process %s %v", pid, cfg.Entrypoint)
		m.processes.Delete(pid)
		processCancelFunc()
		p.errCh <- err
	}()

	return p, nil
}
