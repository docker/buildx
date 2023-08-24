package remote

import (
	"context"
	"io"
	"sync"
	"sync/atomic"

	"github.com/docker/buildx/build"
	"github.com/docker/buildx/controller/pb"
	"github.com/docker/buildx/controller/processes"
	"github.com/moby/buildkit/client"
	"github.com/pkg/errors"
)

type session struct {
	manager *sessionManager

	buildOnGoing atomic.Bool
	statusChan   chan *pb.StatusResponse
	cancelBuild  func()
	buildOptions *pb.BuildOptions
	inputPipe    *io.PipeWriter

	result *build.ResultHandle

	processes *processes.Manager

	mu sync.Mutex
}

func (s *session) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.processes.CancelRunningProcesses()
	if s.cancelBuild != nil {
		s.cancelBuild()
	}
	if s.result != nil {
		s.result.Done()
	}
}

func (s *session) getProcess(id string) (*processes.Process, bool) {
	return s.processes.Get(id)
}

func (s *session) listProcesses() []*pb.ProcessInfo {
	return s.processes.ListProcesses()
}

func (s *session) deleteProcess(id string) error {
	return s.processes.DeleteProcess(id)
}

func (s *session) startProcess(id string, cfg *pb.InvokeConfig) (*processes.Process, error) {
	res := s.result
	return s.processes.StartProcess(id, res, cfg)
}

func (s *session) build(ctx context.Context, options *pb.BuildOptions, wrapBuildError func(error) error) (*client.SolveResponse, error) {
	s.mu.Lock()
	if !s.buildOnGoing.CompareAndSwap(false, true) {
		s.mu.Unlock()
		return nil, errors.New("build ongoing")
	}
	if s.processes != nil {
		s.processes.CancelRunningProcesses()
	}
	s.processes = processes.NewManager()
	s.result = nil
	s.statusChan = make(chan *pb.StatusResponse)

	inR, inW := io.Pipe()
	s.inputPipe = inW

	bCtx, cancel := context.WithCancel(ctx)

	var doneOnce sync.Once
	s.cancelBuild = func() {
		doneOnce.Do(func() {
			s.mu.Lock()
			cancel()
			close(s.statusChan)
			s.statusChan = nil
			s.buildOnGoing.Store(false)
			s.mu.Unlock()
		})
	}
	defer s.cancelBuild()
	s.manager.updateCond.Broadcast()
	s.mu.Unlock()

	resp, res, buildErr := s.manager.buildFunc(bCtx, options, inR, pb.NewProgressWriter(s.statusChan))

	s.mu.Lock()
	// NOTE: buildFunc can return *build.ResultHandle even on error (e.g. when it's implemented using (github.com/docker/buildx/controller/build).RunBuild).
	if res != nil {
		s.result = res
		s.buildOptions = options
		s.manager.updateCond.Broadcast()
		if buildErr != nil {
			buildErr = wrapBuildError(buildErr)
		}
	}
	s.mu.Unlock()

	return resp, buildErr
}

func (s *session) getStatusChan() chan *pb.StatusResponse {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.statusChan
}

func (s *session) getInputWriter() *io.PipeWriter {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inputPipe
}

func (s *session) getBuildOptions() *pb.BuildOptions {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buildOptions
}

func newSessionManager(buildFunc BuildFunc) *sessionManager {
	var mu sync.Mutex
	return &sessionManager{
		updateCond:   sync.NewCond(&mu),
		updateCondMu: &mu,
		buildFunc:    buildFunc,
	}
}

type sessionManager struct {
	sessions  sync.Map
	buildFunc BuildFunc

	updateCond   *sync.Cond
	updateCondMu *sync.Mutex

	created atomic.Bool
}

func (m *sessionManager) newSession(ref string) *session {
	s := &session{manager: m, processes: processes.NewManager()}
	m.add(ref, s)
	return s
}

func (m *sessionManager) add(id string, v *session) {
	m.sessions.Store(id, v)
	m.updateCond.Broadcast()
	m.created.Store(true)
}

func (m *sessionManager) get(id string) *session {
	v, ok := m.sessions.Load(id)
	if !ok {
		return nil
	}
	return v.(*session)
}

func (m *sessionManager) delete(id string) error {
	v, ok := m.sessions.LoadAndDelete(id)
	if !ok {
		return errors.Errorf("unknown session %q", id)
	}
	v.(*session).close()
	return nil
}

func (m *sessionManager) list() (res []string) {
	m.sessions.Range(func(key, value any) bool {
		res = append(res, key.(string))
		return true
	})
	return
}

func (m *sessionManager) close() error {
	m.sessions.Range(func(key, value any) bool {
		value.(*session).close()
		return true
	})
	return nil
}

func (m *sessionManager) waitAndGet(ctx context.Context, id string, f func(s *session) bool) (*session, error) {
	go func() {
		<-ctx.Done()
		m.updateCond.Broadcast()
	}()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		s := m.get(id)
		if s == nil || !f(s) {
			m.updateCondMu.Lock()
			m.updateCond.Wait()
			m.updateCondMu.Unlock()
			continue
		}
		return s, nil
	}
}

func (m *sessionManager) sessionCreated() bool {
	return m.created.Load()
}
