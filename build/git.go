package build

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/docker/buildx/util/gitutil"
	specs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sirupsen/logrus"
)

const DockerfileLabel = "com.docker.image.source.entrypoint"

func getGitAttributes(ctx context.Context, contextPath string, dockerfilePath string) (res map[string]string) {
	res = make(map[string]string)
	if contextPath == "" {
		return
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
		return
	}

	// figure out in which directory the git command needs to run in
	var wd string
	if filepath.IsAbs(contextPath) {
		wd = contextPath
	} else {
		cwd, _ := os.Getwd()
		wd, _ = filepath.Abs(filepath.Join(cwd, contextPath))
	}

	gitc, err := gitutil.New(gitutil.WithContext(ctx), gitutil.WithWorkingDir(wd))
	if err != nil {
		logrus.Warnf("Failed to initialize git: %v", err)
		return
	}

	if !gitc.IsInsideWorkTree() {
		logrus.Warnf("Unable to determine git information")
		return
	}

	var resRevision, resSource, resDockerfilePath string

	if sha, err := gitc.FullCommit(); err == nil && sha != "" {
		resRevision = sha
		if gitc.IsDirty() {
			resRevision += "-dirty"
		}
	}

	if rurl, err := gitc.RemoteURL(); err == nil && rurl != "" {
		resSource = rurl
	}

	if setGitLabels {
		if root, err := gitc.RootDir(); err == nil && root != "" {
			if dockerfilePath == "" {
				dockerfilePath = filepath.Join(wd, "Dockerfile")
			}
			if !filepath.IsAbs(dockerfilePath) {
				cwd, _ := os.Getwd()
				dockerfilePath = filepath.Join(cwd, dockerfilePath)
			}
			dockerfilePath, _ = filepath.Rel(root, dockerfilePath)
			if !strings.HasPrefix(dockerfilePath, "..") {
				resDockerfilePath = dockerfilePath
			}
		}
	}

	if resSource != "" {
		if setGitLabels {
			res["label:"+specs.AnnotationSource] = resSource
		}
		if setGitInfo {
			res["vcs:source"] = resSource
		}
	}
	if resRevision != "" {
		if setGitLabels {
			res["label:"+specs.AnnotationRevision] = resRevision
		}
		if setGitInfo {
			res["vcs:revision"] = resRevision
		}
	}
	if resDockerfilePath != "" {
		if setGitLabels {
			res["label:"+DockerfileLabel] = resDockerfilePath
		}
	}

	return
}
