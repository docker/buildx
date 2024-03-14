package build

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/docker/buildx/util/gitutil"
	"github.com/docker/buildx/util/osutil"
	"github.com/moby/buildkit/client"
	specs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
)

const DockerfileLabel = "com.docker.image.source.entrypoint"

func getGitAttributes(ctx context.Context, contextPath string, dockerfilePath string) (map[string]string, func(key, dir string, so *client.SolveOpt), error) {
	res := make(map[string]string)
	if contextPath == "" {
		return nil, nil, nil
	}

	setGitLabels := false
	if v, ok := os.LookupEnv("BUILDX_GIT_LABELS"); ok {
		if v == "full" { // backward compatibility with old "full" mode
			setGitLabels = true
		} else if v, err := strconv.ParseBool(v); err == nil {
			setGitLabels = v
		}
	}
	setGitInfo := true
	if v, ok := os.LookupEnv("BUILDX_GIT_INFO"); ok {
		if v, err := strconv.ParseBool(v); err == nil {
			setGitInfo = v
		}
	}

	if !setGitLabels && !setGitInfo {
		return nil, nil, nil
	}

	// figure out in which directory the git command needs to run in
	var wd string
	if filepath.IsAbs(contextPath) {
		wd = contextPath
	} else {
		wd, _ = filepath.Abs(filepath.Join(osutil.GetWd(), contextPath))
	}
	wd = osutil.SanitizePath(wd)

	gitc, err := gitutil.New(gitutil.WithContext(ctx), gitutil.WithWorkingDir(wd))
	if err != nil {
		if st, err1 := os.Stat(path.Join(wd, ".git")); err1 == nil && st.IsDir() {
			return res, nil, errors.Wrap(err, "git was not found in the system")
		}
		return nil, nil, nil
	}

	if !gitc.IsInsideWorkTree() {
		if st, err := os.Stat(path.Join(wd, ".git")); err == nil && st.IsDir() {
			return res, nil, errors.New("failed to read current commit information with git rev-parse --is-inside-work-tree")
		}
		return nil, nil, nil
	}

	root, err := gitc.RootDir()
	if err != nil {
		return res, nil, errors.Wrap(err, "failed to get git root dir")
	}

	if sha, err := gitc.FullCommit(); err != nil && !gitutil.IsUnknownRevision(err) {
		return res, nil, errors.Wrap(err, "failed to get git commit")
	} else if sha != "" {
		checkDirty := false
		if v, ok := os.LookupEnv("BUILDX_GIT_CHECK_DIRTY"); ok {
			if v, err := strconv.ParseBool(v); err == nil {
				checkDirty = v
			}
		}
		if checkDirty && gitc.IsDirty() {
			sha += "-dirty"
		}
		if setGitLabels {
			res["label:"+specs.AnnotationRevision] = sha
		}
		if setGitInfo {
			res["vcs:revision"] = sha
		}
	}

	if rurl, err := gitc.RemoteURL(); err == nil && rurl != "" {
		if setGitLabels {
			res["label:"+specs.AnnotationSource] = rurl
		}
		if setGitInfo {
			res["vcs:source"] = rurl
		}
	}

	if setGitLabels && root != "" {
		if dockerfilePath == "" {
			dockerfilePath = filepath.Join(wd, "Dockerfile")
		}
		if !filepath.IsAbs(dockerfilePath) {
			dockerfilePath = filepath.Join(osutil.GetWd(), dockerfilePath)
		}
		if r, err := filepath.Rel(root, dockerfilePath); err == nil && !strings.HasPrefix(r, "..") {
			res["label:"+DockerfileLabel] = r
		}
	}

	return res, func(key, dir string, so *client.SolveOpt) {
		if !setGitInfo || root == "" {
			return
		}
		dir, err := filepath.Abs(dir)
		if err != nil {
			return
		}
		if lp, err := osutil.GetLongPathName(dir); err == nil {
			dir = lp
		}
		dir = osutil.SanitizePath(dir)
		if r, err := filepath.Rel(root, dir); err == nil && !strings.HasPrefix(r, "..") {
			so.FrontendAttrs["vcs:localdir:"+key] = r
		}
	}, nil
}
