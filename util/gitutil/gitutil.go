package gitutil

import (
	"context"
	"net/url"
	"strings"

	bkgitutil "github.com/moby/buildkit/util/gitutil"
	"github.com/pkg/errors"
)

// GitCLI represents an active git object
type GitCLI struct {
	bkgitutil.GitCLI
}

// New initializes a new git client
func New(opts ...bkgitutil.Option) (*GitCLI, error) {
	cli, err := bkgitutil.NewGitCLI(opts...)
	if err != nil {
		return nil, err
	}

	gitpath, err := gitPath(cli.Dir())
	if err != nil {
		return nil, errors.New("git not found in PATH")
	}
	cli = cli.New(bkgitutil.WithGitBinary(gitpath))
	return &GitCLI{*cli}, nil
}

func (cli *GitCLI) IsInsideWorkTree(ctx context.Context) bool {
	out, err := cli.clean(cli.Run(ctx, "rev-parse", "--is-inside-work-tree"))
	return out == "true" && err == nil
}

func (cli *GitCLI) IsDirty(ctx context.Context) bool {
	out, err := cli.Run(ctx, "status", "--porcelain", "--ignored")
	return strings.TrimSpace(string(out)) != "" || err != nil
}

func (cli *GitCLI) RemoteURL(ctx context.Context) (string, error) {
	// Try to get the remote URL from the origin remote first
	if ru, err := cli.clean(cli.Run(ctx, "remote", "get-url", "origin")); err == nil && ru != "" {
		return stripCredentials(ru), nil
	}
	// If that fails, try to get the remote URL from the upstream remote
	if ru, err := cli.clean(cli.Run(ctx, "remote", "get-url", "upstream")); err == nil && ru != "" {
		return stripCredentials(ru), nil
	}
	return "", errors.New("no remote URL found for either origin or upstream")
}

func (cli *GitCLI) FullCommit(ctx context.Context) (string, error) {
	return cli.clean(cli.Run(ctx, "show", "--format=%H", "HEAD", "--quiet", "--"))
}

func (cli *GitCLI) ShortCommit(ctx context.Context) (string, error) {
	return cli.clean(cli.Run(ctx, "show", "--format=%h", "HEAD", "--quiet", "--"))
}

func (cli *GitCLI) Tag(ctx context.Context) (string, error) {
	var tag string
	var err error
	for _, fn := range []func() (string, error){
		func() (string, error) {
			return cli.clean(cli.Run(ctx, "tag", "--points-at", "HEAD", "--sort", "-version:creatordate"))
		},
		func() (string, error) {
			return cli.clean(cli.Run(ctx, "describe", "--tags", "--abbrev=0"))
		},
	} {
		tag, err = fn()
		if tag != "" || err != nil {
			return tag, err
		}
	}
	return tag, err
}

func (cli *GitCLI) clean(dt []byte, err error) (string, error) {
	out := string(dt)
	out = strings.ReplaceAll(strings.Split(out, "\n")[0], "'", "")
	if err != nil {
		err = errors.New(strings.TrimSuffix(err.Error(), "\n"))
	}
	return out, err
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
