package build

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
)

var repoDir string

func setupTest(tb testing.TB) func(tb testing.TB) {
	repoDir = tb.TempDir()
	// required for local testing on mac to avoid strange /private symlinks
	if runtime.GOOS == "darwin" {
		repoDir, _ = filepath.EvalSymlinks(repoDir)
	}
	cmd := exec.Command("git", "init")
	cmd.Dir = repoDir
	err := cmd.Run()
	assert.Nilf(tb, err, "failed to init git repo: %v", err)

	df := []byte("FROM alpine:latest\n")
	err = os.WriteFile(filepath.Join(repoDir, "Dockerfile"), df, 0644)
	assert.Nilf(tb, err, "failed to write file: %v", err)

	cmd = exec.Command("git", "add", "Dockerfile")
	cmd.Dir = repoDir
	err = cmd.Run()
	assert.Nilf(tb, err, "failed to add file: %v", err)

	cmd = exec.Command("git", "config", "user.name", "buildx")
	cmd.Dir = repoDir
	err = cmd.Run()
	assert.Nilf(tb, err, "failed to set git user.name: %v", err)

	cmd = exec.Command("git", "config", "user.email", "buildx@docker.com")
	cmd.Dir = repoDir
	err = cmd.Run()
	assert.Nilf(tb, err, "failed to set git user.email: %v", err)

	cmd = exec.Command("git", "commit", "-m", "Initial commit")
	cmd.Dir = repoDir
	err = cmd.Run()
	assert.Nilf(tb, err, "failed to commit: %v", err)

	return func(tb testing.TB) {
		os.Unsetenv("BUILDX_GIT_LABELS")
		os.RemoveAll(repoDir)
	}
}

func TestAddGitProvenanceDataWithoutEnv(t *testing.T) {
	defer setupTest(t)(t)
	labels, err := addGitProvenance(context.Background(), repoDir, filepath.Join(repoDir, "Dockerfile"))
	assert.Nilf(t, err, "No error expected")
	assert.Nilf(t, labels, "No labels expected")
}

func TestAddGitProvenanceDataWitEmptyEnv(t *testing.T) {
	defer setupTest(t)(t)
	os.Setenv("BUILDX_GIT_LABELS", "")
	labels, err := addGitProvenance(context.Background(), repoDir, filepath.Join(repoDir, "Dockerfile"))
	assert.Nilf(t, err, "No error expected")
	assert.Nilf(t, labels, "No labels expected")
}

func TestAddGitProvenanceDataWithoutLabels(t *testing.T) {
	defer setupTest(t)(t)
	os.Setenv("BUILDX_GIT_LABELS", "full")
	labels, err := addGitProvenance(context.Background(), repoDir, filepath.Join(repoDir, "Dockerfile"))
	assert.Nilf(t, err, "No error expected")
	assert.Equal(t, 2, len(labels), "Exactly 2 git provenance labels expected")
	assert.Equal(t, "Dockerfile", labels[DockerfileLabel], "Expected a dockerfile path provenance label")

	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repoDir
	out, _ := cmd.Output()
	assert.Equal(t, strings.TrimSpace(string(out)), labels[ocispecs.AnnotationRevision], "Expected a sha provenance label")
}

func TestAddGitProvenanceDataWithLabels(t *testing.T) {
	defer setupTest(t)(t)
	// make a change to test dirty flag
	df := []byte("FROM alpine:edge\n")
	os.Mkdir(filepath.Join(repoDir, "dir"), 0755)
	os.WriteFile(filepath.Join(repoDir, "dir", "Dockerfile"), df, 0644)
	// add a remote
	cmd := exec.Command("git", "remote", "add", "origin", "git@github.com:docker/buildx.git")
	cmd.Dir = repoDir
	cmd.Run()

	os.Setenv("BUILDX_GIT_LABELS", "full")
	labels, err := addGitProvenance(context.Background(), repoDir, filepath.Join(repoDir, "Dockerfile"))
	assert.Nilf(t, err, "No error expected")
	assert.Equal(t, 3, len(labels), "Exactly 3 git provenance labels expected")
	assert.Equal(t, "Dockerfile", labels[DockerfileLabel], "Expected a dockerfile path provenance label")
	assert.Equal(t, "git@github.com:docker/buildx.git", labels[ocispecs.AnnotationSource], "Expected a remote provenance label")

	cmd = exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repoDir
	out, _ := cmd.Output()
	assert.Equal(t, fmt.Sprintf("%s-dirty", strings.TrimSpace(string(out))), labels[ocispecs.AnnotationRevision], "Expected a sha provenance label")
}

func TestAddGitProvenanceDataOutsideOfGitRepository(t *testing.T) {
	defer setupTest(t)(t)
	os.Setenv("BUILDX_GIT_LABELS", "full")
	parentDir := filepath.Dir(repoDir)
	cwd, _ := os.Getwd()
	os.Chdir(parentDir)
	labels, err := addGitProvenance(context.Background(), filepath.Base(repoDir), "")
	assert.Nilf(t, err, "No error expected")
	assert.Equal(t, "Dockerfile", labels[DockerfileLabel], "Expected a dockerfile path provenance label")
	os.Chdir(cwd)
}
