package tests

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/containerd/continuity/fs/fstest"
	"github.com/moby/buildkit/frontend/subrequests/lint"
	"github.com/moby/buildkit/frontend/subrequests/outline"
	"github.com/moby/buildkit/frontend/subrequests/targets"
	"github.com/moby/buildkit/util/testutil/integration"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var evalTests = []func(t *testing.T, sb integration.Sandbox){
	testEvalBuild,
}

func testEvalBuild(t *testing.T, sb integration.Sandbox) {
	t.Run("lint", func(t *testing.T) {
		dockerfile := []byte(`
frOM busybox as base
cOpy Dockerfile .
from scratch
COPy --from=base \
  /Dockerfile \
  /
	`)
		dir := tmpdir(
			t,
			fstest.CreateFile("Dockerfile", dockerfile, 0600),
		)

		cmd := buildxCmd(sb, withArgs("eval", "--request=lint,format=json", "build", dir))
		stdout := bytes.Buffer{}
		stderr := bytes.Buffer{}
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		require.NoError(t, cmd.Run(), stdout.String(), stderr.String())

		var res lint.LintResults
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &res))
		require.Equal(t, 3, len(res.Warnings))
	})

	t.Run("outline", func(t *testing.T) {
		dockerfile := []byte(`
FROM busybox AS first
RUN --mount=type=secret,target=/etc/passwd,required=true --mount=type=ssh true

FROM alpine AS second
RUN --mount=type=secret,id=unused --mount=type=ssh,id=ssh2 true

FROM scratch AS third
ARG BAR
RUN --mount=type=secret,id=second${BAR} true

FROM third AS target
COPY --from=first /foo /
RUN --mount=type=ssh,id=ssh3,required true

FROM second
	`)
		dir := tmpdir(
			t,
			fstest.CreateFile("Dockerfile", dockerfile, 0600),
		)

		cmd := buildxCmd(sb, withArgs("eval", "--request=outline,format=json", "build", "--build-arg=BAR=678", "--target=target", dir))
		stdout := bytes.Buffer{}
		stderr := bytes.Buffer{}
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		require.NoError(t, cmd.Run(), stdout.String(), stderr.String())

		var res outline.Outline
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &res))
		assert.Equal(t, "target", res.Name)

		require.Equal(t, 1, len(res.Args))
		assert.Equal(t, "BAR", res.Args[0].Name)
		assert.Equal(t, "678", res.Args[0].Value)

		require.Equal(t, 2, len(res.Secrets))
		assert.Equal(t, "passwd", res.Secrets[0].Name)
		assert.Equal(t, true, res.Secrets[0].Required)
		assert.Equal(t, "second678", res.Secrets[1].Name)
		assert.Equal(t, false, res.Secrets[1].Required)

		require.Equal(t, 2, len(res.SSH))
		assert.Equal(t, "default", res.SSH[0].Name)
		assert.Equal(t, false, res.SSH[0].Required)
		assert.Equal(t, "ssh3", res.SSH[1].Name)
		assert.Equal(t, true, res.SSH[1].Required)

		require.Equal(t, 1, len(res.Sources))
	})

	t.Run("targets", func(t *testing.T) {
		dockerfile := []byte(`
# build defines stage for compiling the binary
FROM alpine AS build
RUN true

FROM busybox as second
RUN false

FROM alpine
RUN false

# binary returns the compiled binary
FROM second AS binary
	`)
		dir := tmpdir(
			t,
			fstest.CreateFile("Dockerfile", dockerfile, 0600),
		)

		cmd := buildxCmd(sb, withArgs("eval", "--request=targets,format=json", "build", dir))
		stdout := bytes.Buffer{}
		stderr := bytes.Buffer{}
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		require.NoError(t, cmd.Run(), stdout.String(), stderr.String())

		var res targets.List
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &res))

		require.Equal(t, 4, len(res.Targets))
		assert.Equal(t, "build", res.Targets[0].Name)
		assert.Equal(t, "defines stage for compiling the binary", res.Targets[0].Description)
		assert.Equal(t, "alpine", res.Targets[0].Base)
		assert.Equal(t, "second", res.Targets[1].Name)
		assert.Empty(t, res.Targets[1].Description)
		assert.Equal(t, "busybox", res.Targets[1].Base)
		assert.Empty(t, res.Targets[2].Name)
		assert.Empty(t, res.Targets[2].Description)
		assert.Equal(t, "alpine", res.Targets[2].Base)
		assert.Equal(t, "binary", res.Targets[3].Name)
		assert.Equal(t, "returns the compiled binary", res.Targets[3].Description)
		assert.Equal(t, "second", res.Targets[3].Base)
		assert.Equal(t, true, res.Targets[3].Default)

		require.Equal(t, 1, len(res.Sources))
	})
}
