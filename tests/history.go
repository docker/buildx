package tests

import (
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/containerd/continuity/fs/fstest"
	"github.com/docker/buildx/util/gitutil"
	"github.com/docker/buildx/util/gitutil/gittestutil"
	bkgitutil "github.com/moby/buildkit/util/gitutil"
	"github.com/moby/buildkit/util/testutil/integration"
	"github.com/stretchr/testify/require"
)

var historyTests = []func(t *testing.T, sb integration.Sandbox){
	testHistoryExport,
	testHistoryExportFinalize,
	testHistoryExportFinalizeMultiNodeRef,
	testHistoryExportFinalizeMultiNodeAll,
	testHistoryInspect,
	testHistoryLs,
	testHistoryRm,
	testHistoryLsStoppedBuilder,
	testHistoryBuildName,
}

func testHistoryExport(t *testing.T, sb integration.Sandbox) {
	ref := buildTestProject(t, sb)
	require.NotEmpty(t, ref.Ref)
	requireHistoryRef(t, sb, ref.Ref)

	outFile := path.Join(t.TempDir(), "export.dockerbuild")
	cmd := buildxCmd(sb, withArgs("history", "export", ref.Ref, "--output", outFile))
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.FileExists(t, outFile)
}

func testHistoryExportFinalize(t *testing.T, sb integration.Sandbox) {
	ref := buildTestProject(t, sb)
	require.NotEmpty(t, ref.Ref)
	requireHistoryRef(t, sb, ref.Ref)

	outFile := path.Join(t.TempDir(), "export.dockerbuild")
	cmd := buildxCmd(sb, withArgs("history", "export", ref.Ref, "--finalize", "--output", outFile))
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.FileExists(t, outFile)
}

func testHistoryExportFinalizeMultiNodeRef(t *testing.T, sb integration.Sandbox) {
	if !isRemoteMultiNodeWorker(sb) {
		t.Skip("only testing with remote multi-node worker")
	}

	ref := buildTestProject(t, sb, withArgs("--platform=linux/amd64,linux/arm64", "--output=type=cacheonly"))
	require.NotEmpty(t, ref.Ref)
	requireHistoryRef(t, sb, ref.Ref)

	outFile := path.Join(t.TempDir(), "export.dockerbuild")
	cmd := buildxCmd(sb, withArgs("history", "export", ref.Ref, "--finalize", "--output", outFile))
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.FileExists(t, outFile)
}

func testHistoryExportFinalizeMultiNodeAll(t *testing.T, sb integration.Sandbox) {
	if !isRemoteMultiNodeWorker(sb) {
		t.Skip("only testing with remote multi-node worker")
	}

	ref := buildTestProject(t, sb, withArgs("--platform=linux/amd64,linux/arm64", "--output=type=cacheonly"))
	require.NotEmpty(t, ref.Ref)
	requireHistoryRef(t, sb, ref.Ref)

	outFile := path.Join(t.TempDir(), "export.dockerbuild")
	cmd := buildxCmd(sb, withArgs("history", "export", "--finalize", "--all", "--output", outFile))
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.FileExists(t, outFile)
}

func testHistoryInspect(t *testing.T, sb integration.Sandbox) {
	ref := buildTestProject(t, sb)
	require.NotEmpty(t, ref.Ref)

	cmd := buildxCmd(sb, withArgs("history", "inspect", ref.Ref, "--format=json"))
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))

	type recT struct {
		Name              string
		Ref               string
		Context           string
		Dockerfile        string
		StartedAt         *time.Time
		CompletedAt       *time.Time
		Duration          time.Duration
		Status            string
		NumCompletedSteps int32
		NumTotalSteps     int32
		NumCachedSteps    int32
	}
	var rec recT
	err = json.Unmarshal(out, &rec)
	require.NoError(t, err)
	require.Equal(t, ref.Ref, rec.Ref)
	require.NotEmpty(t, rec.Name)
}

func testHistoryLs(t *testing.T, sb integration.Sandbox) {
	ref := buildTestProject(t, sb)
	require.NotEmpty(t, ref.Ref)

	rec := requireHistoryRecord(t, sb, ref.String())
	require.Equal(t, ref.String(), rec.Ref)
	require.NotEmpty(t, rec.Name)
}

func testHistoryRm(t *testing.T, sb integration.Sandbox) {
	ref := buildTestProject(t, sb)
	require.NotEmpty(t, ref.Ref)

	cmd := buildxCmd(sb, withArgs("history", "rm", ref.Ref))
	out, err := cmd.Output()
	require.NoError(t, err, string(out))
}

func testHistoryLsStoppedBuilder(t *testing.T, sb integration.Sandbox) {
	if !isDockerContainerWorker(sb) {
		t.Skip("only testing with docker-container worker")
	}

	var builderName string
	t.Cleanup(func() {
		if builderName == "" {
			return
		}
		out, err := rmCmd(sb, withArgs(builderName))
		require.NoError(t, err, out)
	})

	out, err := createCmd(sb, withArgs("--driver", "docker-container"))
	require.NoError(t, err, out)
	builderName = strings.TrimSpace(out)

	ref := buildTestProject(t, sb)
	require.NotEmpty(t, ref.Ref)

	cmd := buildxCmd(sb, withArgs("stop", builderName))
	bout, err := cmd.CombinedOutput()
	require.NoError(t, err, string(bout))

	cmd = buildxCmd(sb, withArgs("history", "ls", "--builder="+builderName, "--filter=ref="+ref.Ref, "--format=json"))
	bout, err = cmd.CombinedOutput()
	require.NoError(t, err, string(bout))
}

func testHistoryBuildName(t *testing.T, sb integration.Sandbox) {
	t.Run("override", func(t *testing.T) {
		dir := createTestProject(t)
		out, err := buildCmd(sb, withArgs("--build-arg=BUILDKIT_BUILD_NAME=foobar", "--metadata-file", filepath.Join(dir, "md.json"), dir))
		require.NoError(t, err, out)

		dt, err := os.ReadFile(filepath.Join(dir, "md.json"))
		require.NoError(t, err)

		type mdT struct {
			BuildRef string `json:"buildx.build.ref"`
		}
		var md mdT
		err = json.Unmarshal(dt, &md)
		require.NoError(t, err)

		refParts := strings.Split(md.BuildRef, "/")
		require.Len(t, refParts, 3)

		rec := requireHistoryRecord(t, sb, md.BuildRef)
		require.Equal(t, md.BuildRef, rec.Ref)
		require.Equal(t, "foobar", rec.Name)
	})

	t.Run("git query", func(t *testing.T) {
		dockerfile := []byte(`
FROM busybox:latest
COPY foo /foo
`)
		dir := tmpdir(
			t,
			fstest.CreateFile("Dockerfile", dockerfile, 0600),
			fstest.CreateFile("foo", []byte("foo"), 0600),
		)
		dirDest := t.TempDir()

		git, err := gitutil.New(bkgitutil.WithDir(dir))
		require.NoError(t, err)

		gittestutil.GitInit(git, t)
		gittestutil.GitAdd(git, t, "Dockerfile", "foo")
		gittestutil.GitCommit(git, t, "initial commit")
		addr := gittestutil.GitServeHTTP(git, t)

		out, err := buildCmd(sb, withDir(dir),
			withArgs("--output=type=local,dest="+dirDest, "--metadata-file", filepath.Join(dir, "md.json"), addr+"?ref=main"),
			withEnv("BUILDX_SEND_GIT_QUERY_AS_INPUT=true"),
		)
		require.NoError(t, err, out)
		require.FileExists(t, filepath.Join(dirDest, "foo"))

		dt, err := os.ReadFile(filepath.Join(dir, "md.json"))
		require.NoError(t, err)

		type mdT struct {
			BuildRef string `json:"buildx.build.ref"`
		}
		var md mdT
		err = json.Unmarshal(dt, &md)
		require.NoError(t, err)

		refParts := strings.Split(md.BuildRef, "/")
		require.Len(t, refParts, 3)

		rec := requireHistoryRecord(t, sb, md.BuildRef)
		require.Equal(t, md.BuildRef, rec.Ref)
		require.Equal(t, addr+"#main", rec.Name)
	})

	t.Run("bake git", func(t *testing.T) {
		bakefile := []byte(`
target "default" {
	dockerfile-inline = <<EOT
FROM scratch
COPY foo /foo
EOT
}
`)
		dir := tmpdir(
			t,
			fstest.CreateFile("docker-bake.hcl", bakefile, 0600),
			fstest.CreateFile("foo", []byte("foo"), 0600),
		)
		dirDest := t.TempDir()

		git, err := gitutil.New(bkgitutil.WithDir(dir))
		require.NoError(t, err)

		gittestutil.GitInit(git, t)
		gittestutil.GitAdd(git, t, "docker-bake.hcl", "foo")
		gittestutil.GitCommit(git, t, "initial commit")
		addr := gittestutil.GitServeHTTP(git, t)

		out, err := bakeCmd(sb, withDir(dir),
			withArgs(addr, "--set", "*.output=type=local,dest="+dirDest, "--metadata-file", filepath.Join(dir, "md.json")),
		)
		require.NoError(t, err, out)
		require.FileExists(t, filepath.Join(dirDest, "foo"))

		dt, err := os.ReadFile(filepath.Join(dir, "md.json"))
		require.NoError(t, err)

		type mdT struct {
			Default struct {
				BuildRef string `json:"buildx.build.ref"`
			} `json:"default"`
		}
		var md mdT
		err = json.Unmarshal(dt, &md)
		require.NoError(t, err)

		refParts := strings.Split(md.Default.BuildRef, "/")
		require.Len(t, refParts, 3)

		rec := requireHistoryRecord(t, sb, md.Default.BuildRef)
		require.Equal(t, md.Default.BuildRef, rec.Ref)
		require.Equal(t, addr, rec.Name)
	})
}

type buildRef struct {
	Builder string
	Node    string
	Ref     string
}

func (b buildRef) String() string {
	return b.Builder + "/" + b.Node + "/" + b.Ref
}

func buildTestProject(t *testing.T, sb integration.Sandbox, opts ...cmdOpt) buildRef {
	dir := createTestProject(t)
	opts = append(opts, withArgs("--metadata-file", filepath.Join(dir, "md.json"), dir))
	out, err := buildCmd(sb, opts...)
	require.NoError(t, err, out)

	dt, err := os.ReadFile(filepath.Join(dir, "md.json"))
	require.NoError(t, err)

	type mdT struct {
		BuildRef string `json:"buildx.build.ref"`
	}
	var md mdT
	err = json.Unmarshal(dt, &md)
	require.NoError(t, err)

	refParts := strings.Split(md.BuildRef, "/")
	require.Len(t, refParts, 3)

	return buildRef{
		Builder: refParts[0],
		Node:    refParts[1],
		Ref:     refParts[2],
	}
}

func requireHistoryRef(t *testing.T, sb integration.Sandbox, ref string) {
	cmd := buildxCmd(sb, withArgs("history", "ls", "--format={{.Ref}}"))
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	var matches int
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		if strings.TrimSpace(line) == ref {
			matches++
		}
	}
	require.GreaterOrEqual(t, matches, 1)
}

type historyLsRecord struct {
	Ref            string     `json:"ref"`
	Name           string     `json:"name"`
	Status         string     `json:"status"`
	CreatedAt      *time.Time `json:"created_at"`
	CompletedAt    *time.Time `json:"completed_at"`
	TotalSteps     int32      `json:"total_steps"`
	CompletedSteps int32      `json:"completed_steps"`
	CachedSteps    int32      `json:"cached_steps"`
}

func requireHistoryRecord(t *testing.T, sb integration.Sandbox, ref string, opts ...cmdOpt) historyLsRecord {
	cmd := buildxCmd(sb, append(opts, withArgs("history", "ls", "--format=json"))...)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))

	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var rec historyLsRecord
		err := json.Unmarshal([]byte(line), &rec)
		require.NoError(t, err)
		if rec.Ref == ref {
			return rec
		}
	}

	require.Failf(t, "history record not found", "ref %q was not found in history ls output:\n%s", ref, string(out))
	return historyLsRecord{}
}
