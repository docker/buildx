package gitutil

import (
	"bytes"
	"context"
	"os/exec"
	"strings"

	"github.com/pkg/errors"
)

// Git represents an active git object
type Git struct {
	ctx     context.Context
	wd      string
	gitpath string
}

// Option provides a variadic option for configuring the git client.
type Option func(b *Git)

// WithContext sets context.
func WithContext(ctx context.Context) Option {
	return func(b *Git) {
		b.ctx = ctx
	}
}

// WithWorkingDir sets working directory.
func WithWorkingDir(wd string) Option {
	return func(b *Git) {
		b.wd = wd
	}
}

// New initializes a new git client
func New(opts ...Option) (*Git, error) {
	var err error
	c := &Git{
		ctx: context.Background(),
	}

	for _, opt := range opts {
		opt(c)
	}

	c.gitpath, err = gitPath(c.wd)
	if err != nil {
		return nil, errors.New("git not found in PATH")
	}

	return c, nil
}

func (c *Git) IsInsideWorkTree() bool {
	out, err := c.clean(c.run("rev-parse", "--is-inside-work-tree"))
	return out == "true" && err == nil
}

func (c *Git) IsDirty() bool {
	out, err := c.run("status", "--porcelain", "--ignored")
	return strings.TrimSpace(out) != "" || err != nil
}

func (c *Git) RootDir() (string, error) {
	return c.clean(c.run("rev-parse", "--show-toplevel"))
}

func (c *Git) RemoteURL() (string, error) {
	// Try to get the remote URL from the origin remote first
	if ru, err := c.clean(c.run("remote", "get-url", "origin")); err == nil && ru != "" {
		return ru, nil
	}
	// If that fails, try to get the remote URL from the upstream remote
	if ru, err := c.clean(c.run("remote", "get-url", "upstream")); err == nil && ru != "" {
		return ru, nil
	}
	return "", errors.New("no remote URL found for either origin or upstream")
}

func (c *Git) FullCommit() (string, error) {
	return c.clean(c.run("show", "--format=%H", "HEAD", "--quiet"))
}

func (c *Git) ShortCommit() (string, error) {
	return c.clean(c.run("show", "--format=%h", "HEAD", "--quiet"))
}

func (c *Git) Tag() (string, error) {
	var tag string
	var err error
	for _, fn := range []func() (string, error){
		func() (string, error) {
			return c.clean(c.run("tag", "--points-at", "HEAD", "--sort", "-version:creatordate"))
		},
		func() (string, error) {
			return c.clean(c.run("describe", "--tags", "--abbrev=0"))
		},
	} {
		tag, err = fn()
		if tag != "" || err != nil {
			return tag, err
		}
	}
	return tag, err
}

func (c *Git) run(args ...string) (string, error) {
	var extraArgs = []string{
		"-c", "log.showSignature=false",
	}

	args = append(extraArgs, args...)
	cmd := exec.CommandContext(c.ctx, c.gitpath, args...)
	if c.wd != "" {
		cmd.Dir = c.wd
	}

	stdout := bytes.Buffer{}
	stderr := bytes.Buffer{}
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", errors.New(stderr.String())
	}
	return stdout.String(), nil
}

func (c *Git) clean(out string, err error) (string, error) {
	out = strings.ReplaceAll(strings.Split(out, "\n")[0], "'", "")
	if err != nil {
		err = errors.New(strings.TrimSuffix(err.Error(), "\n"))
	}
	return out, err
}
