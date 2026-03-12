package gitutil

import (
	"context"
	"net/url"
	"strings"

	"github.com/docker/buildx/util/osutil"
	bkgitutil "github.com/moby/buildkit/util/gitutil"
	"github.com/pkg/errors"
)

// Git represents an active git object.
type Git struct {
	ctx context.Context
	cli *bkgitutil.GitCLI
}

// Option provides a variadic option for configuring the git client.
type Option func(*Git)

// WithContext sets context.
func WithContext(ctx context.Context) Option {
	return func(g *Git) {
		g.ctx = ctx
	}
}

// WithWorkingDir sets working directory.
func WithWorkingDir(wd string) Option {
	return func(g *Git) {
		if g.cli == nil {
			g.cli = bkgitutil.NewGitCLI()
		}
		g.cli = g.cli.New(bkgitutil.WithDir(wd))
	}
}

// New initializes a new git client.
func New(opts ...Option) (*Git, error) {
	g := &Git{
		ctx: context.Background(),
		cli: bkgitutil.NewGitCLI(),
	}

	for _, opt := range opts {
		opt(g)
	}

	gitpath, err := gitPath(g.cli.Dir())
	if err != nil {
		return nil, err
	}

	g.cli = g.cli.New(
		bkgitutil.WithGitBinary(gitpath),
		bkgitutil.WithArgs("-c", "log.showSignature=false"),
	)
	return g, nil
}

func (g *Git) IsInsideWorkTree() bool {
	out, err := g.Run("rev-parse", "--is-inside-work-tree")
	return out == "true" && err == nil
}

func (g *Git) IsDirty() bool {
	out, err := g.Run("status", "--porcelain", "--ignored")
	return strings.TrimSpace(out) != "" || err != nil
}

func (g *Git) RootDir() (string, error) {
	root, err := g.cli.WorkTree(g.ctx)
	if err != nil {
		return "", err
	}
	return osutil.SanitizePath(root), nil
}

func (g *Git) GitDir() (string, error) {
	dir, err := g.cli.GitDir(g.ctx)
	if err != nil {
		return "", err
	}
	return osutil.SanitizePath(dir), nil
}

func (g *Git) RemoteURL() (string, error) {
	// Try default remote based on remote tracking branch.
	if remote, err := g.currentRemote(); err == nil && remote != "" {
		if ru, err := g.Run("remote", "get-url", remote); err == nil && ru != "" {
			return stripCredentials(ru), nil
		}
	}
	// Next try to get the remote URL from the origin remote first.
	if ru, err := g.Run("remote", "get-url", "origin"); err == nil && ru != "" {
		return stripCredentials(ru), nil
	}
	// If that fails, try to get the remote URL from the upstream remote.
	if ru, err := g.Run("remote", "get-url", "upstream"); err == nil && ru != "" {
		return stripCredentials(ru), nil
	}
	return "", errors.New("no remote URL found for either origin or upstream")
}

func (g *Git) FullCommit() (string, error) {
	return g.Run("show", "--format=%H", "HEAD", "--quiet", "--")
}

func (g *Git) ShortCommit() (string, error) {
	return g.Run("show", "--format=%h", "HEAD", "--quiet", "--")
}

func (g *Git) Tag() (string, error) {
	var tag string
	var err error
	for _, fn := range []func() (string, error){
		func() (string, error) {
			return g.Run("tag", "--points-at", "HEAD", "--sort", "-version:creatordate")
		},
		func() (string, error) {
			return g.Run("describe", "--tags", "--abbrev=0")
		},
	} {
		tag, err = fn()
		if tag != "" || err != nil {
			return tag, err
		}
	}
	return tag, err
}

func (g *Git) Run(args ...string) (string, error) {
	return g.clean(g.cli.Run(g.ctx, args...))
}

func (g *Git) clean(dt []byte, err error) (string, error) {
	out := strings.ReplaceAll(strings.Split(string(dt), "\n")[0], "'", "")
	if err != nil {
		msg := strings.TrimSuffix(err.Error(), "\n")
		if stderr, ok := strings.CutPrefix(msg, "git stderr:\n"); ok {
			if idx := strings.LastIndex(stderr, ": exit status "); idx != -1 {
				stderr = stderr[:idx]
			}
			msg = strings.TrimSuffix(stderr, "\n")
		}
		err = errors.New(msg)
	}
	return out, err
}

func (g *Git) currentRemote() (string, error) {
	symref, err := g.Run("symbolic-ref", "-q", "HEAD")
	if err != nil {
		return "", err
	}
	if symref == "" {
		return "", nil
	}
	remote, err := g.Run("for-each-ref", "--format=%(upstream:remotename)", symref)
	if err != nil {
		return "", err
	}
	return remote, nil
}

func IsUnknownRevision(err error) bool {
	if err == nil {
		return false
	}
	// https://github.com/git/git/blob/a6a323b31e2bcbac2518bddec71ea7ad558870eb/setup.c#L204
	errMsg := strings.ToLower(err.Error())
	return strings.Contains(errMsg, "unknown revision or path not in the working tree") || strings.Contains(errMsg, "bad revision")
}

// stripCredentials takes a URL and strips username and password from it.
// e.g. "https://user:password@host.tld/path.git" will be changed to
// "https://host.tld/path.git".
// TODO: remove this function once fix from BuildKit is vendored here
func stripCredentials(s string) string {
	ru, err := url.Parse(s)
	if err != nil {
		return s // string is not a URL, just return it
	}
	ru.User = nil
	return ru.String()
}
