package build

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const DockerfileLabel = "com.docker.image.source.entrypoint"

func addGitProvenance(ctx context.Context, contextPath string, dockerfilePath string) (map[string]string, error) {
	v := os.Getenv("BUILDX_GIT_LABELS")
	if (v != "1" && v != "full") || contextPath == "" {
		return nil, nil
	}
	labels := make(map[string]string, 0)

	// figure out in which directory the git command needs to run in
	var wd string
	if filepath.IsAbs(contextPath) {
		wd = contextPath
	} else {
		cwd, _ := os.Getwd()
		wd, _ = filepath.Abs(filepath.Join(cwd, contextPath))
	}

	// check if inside git working tree
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = wd
	err := cmd.Run()
	if err != nil {
		logrus.Warnf("Unable to determine Git information")
		return nil, nil
	}

	// obtain Git sha of current HEAD
	cmd = exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	cmd.Dir = wd
	out, err := cmd.Output()
	if err != nil {
		return nil, errors.Wrap(err, "error obtaining git head")
	}
	sha := strings.TrimSpace(string(out))

	// check if the current HEAD is clean
	cmd = exec.CommandContext(ctx, "git", "status", "--porcelain", "--ignored")
	cmd.Dir = wd
	out, err = cmd.Output()
	if err != nil {
		return nil, errors.Wrap(err, "error obtaining git status")
	}
	if len(strings.TrimSpace(string(out))) != 0 {
		sha += "-dirty"
	}
	labels[ocispecs.AnnotationRevision] = sha

	// add a remote url if full Git details are requested; if there aren't any remotes don't fail
	if v == "full" {
		cmd = exec.CommandContext(ctx, "git", "ls-remote", "--get-url")
		cmd.Dir = wd
		out, _ := cmd.Output()
		if len(out) > 0 {
			labels[ocispecs.AnnotationSource] = strings.TrimSpace(string(out))
		}
	}

	// add Dockerfile path; there is no org.opencontainers annotation for this
	if dockerfilePath == "" {
		dockerfilePath = filepath.Join(wd, "Dockerfile")
	}

	// obtain Git root directory
	cmd = exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel")
	cmd.Dir = wd
	out, err = cmd.Output()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get git root")
	}
	root := strings.TrimSpace(string(out))

	// record only Dockerfile paths that are within the Git root
	if !filepath.IsAbs(dockerfilePath) {
		cwd, _ := os.Getwd()
		dockerfilePath = filepath.Join(cwd, dockerfilePath)
	}
	dockerfilePath, _ = filepath.Rel(root, dockerfilePath)
	if !strings.HasPrefix(dockerfilePath, "..") {
		labels[DockerfileLabel] = dockerfilePath
	}

	return labels, nil
}
