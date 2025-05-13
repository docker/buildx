package tests

import (
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/moby/buildkit/util/testutil/integration"
	"github.com/stretchr/testify/require"
)

var historyTests = []func(t *testing.T, sb integration.Sandbox){
	testHistoryExport,
	testHistoryExportFinalize,
	testHistoryInspect,
	testHistoryLs,
	testHistoryRm,
}

func testHistoryExport(t *testing.T, sb integration.Sandbox) {
	ref := buildTestProject(t, sb)
	require.NotEmpty(t, ref.Ref)

	outFile := path.Join(t.TempDir(), "export.dockerbuild")
	cmd := buildxCmd(sb, withArgs("history", "export", ref.Ref, "--output", outFile))
	out, err := cmd.Output()
	require.NoError(t, err, string(out))
	require.FileExists(t, outFile)
}

func testHistoryExportFinalize(t *testing.T, sb integration.Sandbox) {
	ref := buildTestProject(t, sb)
	require.NotEmpty(t, ref.Ref)

	outFile := path.Join(t.TempDir(), "export.dockerbuild")
	cmd := buildxCmd(sb, withArgs("history", "export", ref.Ref, "--finalize", "--output", outFile))
	out, err := cmd.Output()
	require.NoError(t, err, string(out))
	require.FileExists(t, outFile)
}

func testHistoryInspect(t *testing.T, sb integration.Sandbox) {
	ref := buildTestProject(t, sb)
	require.NotEmpty(t, ref.Ref)

	cmd := buildxCmd(sb, withArgs("history", "inspect", ref.Ref, "--format=json"))
	out, err := cmd.Output()
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

	cmd := buildxCmd(sb, withArgs("history", "ls", "--filter=ref="+ref.Ref, "--format=json"))
	out, err := cmd.Output()
	require.NoError(t, err, string(out))

	type recT struct {
		Ref            string     `json:"ref"`
		Name           string     `json:"name"`
		Status         string     `json:"status"`
		CreatedAt      *time.Time `json:"created_at"`
		CompletedAt    *time.Time `json:"completed_at"`
		TotalSteps     int32      `json:"total_steps"`
		CompletedSteps int32      `json:"completed_steps"`
		CachedSteps    int32      `json:"cached_steps"`
	}
	var rec recT
	err = json.Unmarshal(out, &rec)
	require.NoError(t, err)
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

type buildRef struct {
	Builder string
	Node    string
	Ref     string
}

func (b buildRef) String() string {
	return b.Builder + "/" + b.Node + "/" + b.Ref
}

func buildTestProject(t *testing.T, sb integration.Sandbox) buildRef {
	dir := createTestProject(t)
	out, err := buildCmd(sb, withArgs("--metadata-file", filepath.Join(dir, "md.json"), dir))
	require.NoError(t, err, string(out))

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
