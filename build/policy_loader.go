package build

import (
	"bytes"
	"context"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/docker/buildx/policy"
	"github.com/docker/buildx/util/sourcemeta"
	"github.com/moby/buildkit/client/llb"
	gwclient "github.com/moby/buildkit/frontend/gateway/client"
	"github.com/pkg/errors"
	"github.com/tonistiigi/fsutil/types"
)

type loadedPolicyOpt struct {
	Files []policy.File
	FS    func() (fs.StatFS, func() error, error)
	policyEvalOpt
}

func resolvePolicyOpts(ctx context.Context, in []policyOpt, resolver *sourcemeta.Resolver) ([]loadedPolicyOpt, error) {
	if len(in) == 0 {
		return nil, nil
	}

	out := make([]loadedPolicyOpt, 0, len(in))
	for _, popt := range in {
		provider := newPolicyPathFS(ctx, resolver, popt)
		loaded := loadedPolicyOpt{
			policyEvalOpt: popt.policyEvalOpt,
			FS:            provider,
		}
		for _, f := range popt.Files {
			if f.Data != nil {
				loaded.Files = append(loaded.Files, policy.File{
					Filename: f.Filename,
					Data:     f.Data,
				})
				continue
			}
			dt, ok, err := loadPolicyData(provider, f.Filename)
			if err != nil {
				return nil, err
			}
			if !ok {
				if f.Optional {
					continue
				}
				return nil, errors.Errorf("policy file %s not found", f.Filename)
			}
			loaded.Files = append(loaded.Files, policy.File{
				Filename: f.Filename,
				Data:     dt,
			})
		}
		if len(loaded.Files) > 0 {
			out = append(out, loaded)
		}
	}

	return out, nil
}

func loadPolicyData(provider func() (fs.StatFS, func() error, error), filename string) ([]byte, bool, error) {
	root, closeFS, err := provider()
	if err != nil {
		return nil, false, errors.Wrapf(err, "failed to get policy FS for %s", filename)
	}
	if closeFS != nil {
		defer closeFS()
	}
	if root == nil {
		return nil, false, nil
	}
	if _, err := root.Stat(filename); err != nil {
		if isFileNotFoundError(err) {
			return nil, false, nil
		}
		return nil, false, errors.Wrapf(err, "failed to stat policy file %s", filename)
	}
	dt, err := fs.ReadFile(root, filename)
	if err != nil {
		if isFileNotFoundError(err) {
			return nil, false, nil
		}
		return nil, false, errors.Wrapf(err, "failed to read policy file %s", filename)
	}
	return dt, true, nil
}

type policyPathFS struct {
	ctx          context.Context
	resolver     *sourcemeta.Resolver
	contextDir   string
	contextState *llb.State

	cwdFS     memoizedPolicyFS
	contextFS memoizedPolicyFS
}

func newPolicyPathFS(ctx context.Context, resolver *sourcemeta.Resolver, popt policyOpt) func() (fs.StatFS, func() error, error) {
	p := &policyPathFS{
		ctx:          context.WithoutCancel(ctx),
		resolver:     resolver,
		contextDir:   popt.ContextDir,
		contextState: popt.ContextState,
	}

	p.cwdFS.init = func() (fs.StatFS, func() error, error) {
		root, err := os.OpenRoot(".")
		if err != nil {
			return nil, nil, err
		}
		baseFS := root.FS()
		statFS, ok := baseFS.(fs.StatFS)
		if !ok {
			root.Close()
			return nil, nil, errors.Errorf("invalid root FS type %T", baseFS)
		}
		return statFS, root.Close, nil
	}

	p.contextFS.init = func() (fs.StatFS, func() error, error) {
		if p.contextState != nil {
			if resolver == nil {
				return nil, nil, errors.New("policy resolver is not configured")
			}
			return newRemotePolicyFS(p.ctx, resolver, *p.contextState), nil, nil
		}
		if p.contextDir == "" {
			return nil, nil, nil
		}
		root, err := os.OpenRoot(p.contextDir)
		if err != nil {
			return nil, nil, err
		}
		baseFS := root.FS()
		statFS, ok := baseFS.(fs.StatFS)
		if !ok {
			root.Close()
			return nil, nil, errors.Errorf("invalid root FS type %T", baseFS)
		}
		return statFS, root.Close, nil
	}

	return func() (fs.StatFS, func() error, error) {
		ref := &policyPathFSRef{policyPathFS: p}
		return ref, ref.Close, nil
	}
}

type policyPathFSRef struct {
	*policyPathFS
	mu           sync.Mutex
	cwdRoot      fs.StatFS
	cwdClose     func() error
	contextRoot  fs.StatFS
	contextClose func() error
}

func (p *policyPathFSRef) Open(name string) (fs.File, error) {
	backend, target, err := p.resolve(name)
	if err != nil {
		return nil, err
	}
	if backend == nil {
		return nil, fs.ErrNotExist
	}
	return backend.Open(target)
}

func (p *policyPathFSRef) Stat(name string) (fs.FileInfo, error) {
	backend, target, err := p.resolve(name)
	if err != nil {
		return nil, err
	}
	if backend == nil {
		return nil, fs.ErrNotExist
	}
	return backend.Stat(target)
}

func (p *policyPathFSRef) resolve(name string) (fs.StatFS, string, error) {
	if name == "" {
		return nil, "", errors.New("policy filename is empty")
	}
	if v, ok := strings.CutPrefix(name, "cwd://"); ok {
		if v == "" {
			return nil, "", errors.Errorf("invalid policy filename %q", name)
		}
		cwd, err := p.getCwdFS()
		if err != nil {
			return nil, "", err
		}
		return cwd, path.Clean(filepath.ToSlash(v)), nil
	}

	contextFS, err := p.getContextFS()
	if err != nil {
		return nil, "", err
	}
	if p.contextState != nil {
		target, err := normalizeRemotePolicyPath(name)
		if err != nil {
			return nil, "", err
		}
		return contextFS, target, nil
	}
	return contextFS, normalizeLocalPolicyPath(name, p.contextDir), nil
}

func (p *policyPathFSRef) getCwdFS() (fs.StatFS, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cwdClose != nil {
		return p.cwdRoot, nil
	}
	cwd, err := p.cwdFS.get()
	if err != nil {
		return nil, err
	}
	p.cwdRoot = cwd
	p.cwdClose = p.cwdFS.close
	return cwd, nil
}

func (p *policyPathFSRef) getContextFS() (fs.StatFS, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.contextClose != nil {
		return p.contextRoot, nil
	}
	contextFS, err := p.contextFS.get()
	if err != nil {
		return nil, err
	}
	p.contextRoot = contextFS
	p.contextClose = p.contextFS.close
	return contextFS, nil
}

func (p *policyPathFSRef) Close() error {
	p.mu.Lock()
	cwdClose := p.cwdClose
	contextClose := p.contextClose
	p.cwdRoot = nil
	p.contextRoot = nil
	p.cwdClose = nil
	p.contextClose = nil
	p.mu.Unlock()

	var firstErr error
	if cwdClose != nil {
		if err := cwdClose(); err != nil {
			firstErr = err
		}
	}
	if contextClose != nil {
		if err := contextClose(); err != nil {
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func normalizeLocalPolicyPath(name, contextDir string) string {
	if filepath.IsAbs(name) && contextDir != "" {
		if rel, err := filepath.Rel(contextDir, name); err == nil {
			if rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return path.Clean(filepath.ToSlash(rel))
			}
		}
	}
	return path.Clean(filepath.ToSlash(name))
}

type memoizedPolicyFS struct {
	init    func() (fs.StatFS, func() error, error)
	mu      sync.Mutex
	loaded  bool
	refs    int
	fs      fs.StatFS
	closeFn func() error
	err     error
}

func (m *memoizedPolicyFS) get() (fs.StatFS, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.loaded {
		m.loaded = true
		if m.init != nil {
			m.fs, m.closeFn, m.err = m.init()
		}
	}
	if m.err != nil {
		return nil, m.err
	}
	m.refs++
	return m.fs, nil
}

func (m *memoizedPolicyFS) close() error {
	m.mu.Lock()
	if m.refs > 0 {
		m.refs--
	}
	if m.refs > 0 {
		m.mu.Unlock()
		return nil
	}
	closeFn := m.closeFn
	m.fs = nil
	m.closeFn = nil
	m.err = nil
	m.loaded = false
	m.mu.Unlock()

	if closeFn != nil {
		return closeFn()
	}
	return nil
}

func normalizeRemotePolicyPath(raw string) (string, error) {
	clean := strings.TrimPrefix(path.Join("/", filepath.ToSlash(raw)), "/")
	if clean == "." || clean == "" {
		return "", errors.Errorf("invalid remote policy filename %q", raw)
	}
	return clean, nil
}

func isFileNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, fs.ErrNotExist) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not found") || strings.Contains(msg, "no such file")
}

type remotePolicyFS struct {
	ctx      context.Context
	resolver *sourcemeta.Resolver
	state    llb.State

	once sync.Once
	ref  gwclient.Reference
	err  error
}

func newRemotePolicyFS(ctx context.Context, resolver *sourcemeta.Resolver, state llb.State) *remotePolicyFS {
	return &remotePolicyFS{
		ctx:      context.WithoutCancel(ctx),
		resolver: resolver,
		state:    state,
	}
}

func (r *remotePolicyFS) Open(name string) (fs.File, error) {
	p, err := normalizeRemotePolicyPath(name)
	if err != nil {
		return nil, err
	}
	ref, err := r.resolveRef()
	if err != nil {
		return nil, err
	}

	st, err := ref.StatFile(r.ctx, gwclient.StatRequest{Path: p})
	if err != nil {
		return nil, err
	}
	dt, err := ref.ReadFile(r.ctx, gwclient.ReadRequest{Filename: p})
	if err != nil {
		return nil, err
	}
	fi := policyFileInfo{
		name: path.Base(p),
		size: int64(len(dt)),
		mode: fs.FileMode(st.Mode),
		tm:   time.Unix(0, st.ModTime),
	}
	if fi.size == 0 {
		fi.size = st.Size
	}

	return &policyReadFile{
		Reader: bytes.NewReader(dt),
		info:   fi,
	}, nil
}

func (r *remotePolicyFS) Stat(name string) (fs.FileInfo, error) {
	p, err := normalizeRemotePolicyPath(name)
	if err != nil {
		return nil, err
	}
	ref, err := r.resolveRef()
	if err != nil {
		return nil, err
	}
	st, err := ref.StatFile(r.ctx, gwclient.StatRequest{Path: p})
	if err != nil {
		return nil, err
	}
	return policyFileInfo{
		name: path.Base(p),
		size: st.Size,
		mode: fs.FileMode(st.Mode),
		tm:   time.Unix(0, st.ModTime),
	}, nil
}

func (r *remotePolicyFS) resolveRef() (gwclient.Reference, error) {
	r.once.Do(func() {
		r.ref, r.err = r.resolver.ResolveState(r.ctx, r.state)
	})
	if r.err != nil {
		return nil, r.err
	}
	return r.ref, nil
}

type policyReadFile struct {
	*bytes.Reader
	info policyFileInfo
}

func (f *policyReadFile) Stat() (fs.FileInfo, error) {
	return f.info, nil
}

func (f *policyReadFile) Close() error {
	return nil
}

type policyFileInfo struct {
	name string
	size int64
	mode fs.FileMode
	tm   time.Time
}

func (i policyFileInfo) Name() string       { return i.name }
func (i policyFileInfo) Size() int64        { return i.size }
func (i policyFileInfo) Mode() fs.FileMode  { return i.mode }
func (i policyFileInfo) ModTime() time.Time { return i.tm }
func (i policyFileInfo) IsDir() bool        { return i.mode.IsDir() }
func (i policyFileInfo) Sys() any {
	return &types.Stat{Mode: uint32(i.mode), Size: i.size, ModTime: i.tm.UnixNano()}
}

var _ fs.StatFS = (*policyPathFSRef)(nil)
var _ fs.StatFS = (*remotePolicyFS)(nil)
var _ fs.File = (*policyReadFile)(nil)
var _ io.ReaderAt = (*bytes.Reader)(nil)
