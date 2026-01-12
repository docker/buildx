package tests

import (
	"path/filepath"
	"testing"

	"github.com/containerd/continuity/fs/fstest"
	"github.com/moby/buildkit/util/testutil/integration"
	"github.com/stretchr/testify/require"
)

var policyBuildTests = []func(t *testing.T, sb integration.Sandbox){
	testBuildPolicyAllow,
	testBuildPolicyDeny,
}

func testBuildPolicyAllow(t *testing.T, sb integration.Sandbox) {
	dockerfile := []byte(`
FROM busybox:latest
RUN echo policy-ok
`)
	policyFile := []byte(`
package docker

default decision = {"allow": true}
`)
	dir := tmpdir(
		t,
		fstest.CreateFile("Dockerfile", dockerfile, 0600),
		fstest.CreateFile("policy.rego", policyFile, 0600),
	)
	policyPath := filepath.Join(dir, "policy.rego")

	cmd := buildxCmd(sb, withDir(dir), withArgs(
		"build",
		"--progress=plain",
		"--policy", "filename="+policyPath,
		"--output=type=cacheonly",
		dir,
	))
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.Contains(t, string(out), "loading policies "+policyPath)
}

func testBuildPolicyDeny(t *testing.T, sb integration.Sandbox) {
	dockerfile := []byte(`
FROM busybox:latest
RUN echo policy-nope
`)
	policyFile := []byte(`
package docker

default decision = {"allow": false, "deny_msg": ["denied by test"]}
`)
	dir := tmpdir(
		t,
		fstest.CreateFile("Dockerfile", dockerfile, 0600),
		fstest.CreateFile("policy.rego", policyFile, 0600),
	)
	policyPath := filepath.Join(dir, "policy.rego")

	cmd := buildxCmd(sb, withDir(dir), withArgs(
		"build",
		"--progress=plain",
		"--policy", "filename="+policyPath,
		"--output=type=cacheonly",
		dir,
	))
	out, err := cmd.CombinedOutput()
	require.Error(t, err, string(out))
	require.Contains(t, string(out), "loading policies "+policyPath)
	require.Contains(t, string(out), "policy decision for source")
	require.Contains(t, string(out), "DENY")
}
