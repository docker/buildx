package tests

import (
	"testing"

	"github.com/containerd/continuity/fs/fstest"
	"github.com/moby/buildkit/util/testutil/integration"
	"github.com/stretchr/testify/require"
)

var policyTestTests = []func(t *testing.T, sb integration.Sandbox){
	testPolicyTestRunFilter,
	testPolicyTestFailMissingInput,
	testPolicyTestNestedPath,
}

func testPolicyTestRunFilter(t *testing.T, sb integration.Sandbox) {
	skipNoCompatBuildKit(t, sb, ">= 0.26.0-0", "policy input requires BuildKit v0.26.0+")
	dir := tmpdir(
		t,
		fstest.CreateFile("policy.rego", []byte(`
package docker

default allow = false

allow if input.image.repo == "example/allowlist"

allow if {
	input.image.repo == "example/docs"
	input.image.tag == "doc"
}

deny_msg[msg] if {
	not allow
	msg := "repository not allowed"
}

decision := {"allow": allow, "deny_msg": deny_msg}
`), 0600),
		fstest.CreateFile("policy_test.rego", []byte(`
package docker

# Images from allowlisted repo are allowed
test_allowlisted_repo if {
	result := data.docker.decision with input as {"image": {"repo": "example/allowlist"}}
	result.allow
	count(result.deny_msg) == 0
}

# Other repos are denied
test_non_allowlisted_repo if {
	result := data.docker.decision with input as {"image": {"repo": "example/blocked"}}
	not result.allow
	result.deny_msg["repository not allowed"]
}

# Docs images are allowed from a specific repo
test_docs_tag_allowed if {
	result := data.docker.decision with input as {"image": {"repo": "example/docs", "tag": "doc"}}
	result.allow
}
`), 0600),
	)

	cmd := buildxCmd(sb, withDir(dir), withArgs(
		"policy",
		"test",
		"--filename",
		"policy",
		".",
	))
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.Contains(t, string(out), "test_allowlisted_repo: PASS")
	require.Contains(t, string(out), "test_non_allowlisted_repo: PASS")
	require.Contains(t, string(out), "test_docs_tag_allowed: PASS")

	cmd = buildxCmd(sb, withDir(dir), withArgs(
		"policy",
		"test",
		"--filename",
		"policy",
		"--run",
		"test_allowlisted_repo",
		".",
	))
	out, err = cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.Contains(t, string(out), "test_allowlisted_repo: PASS")
	require.NotContains(t, string(out), "test_guest")
}

func testPolicyTestFailMissingInput(t *testing.T, sb integration.Sandbox) {
	skipNoCompatBuildKit(t, sb, ">= 0.26.0-0", "policy input requires BuildKit v0.26.0+")
	dir := tmpdir(
		t,
		fstest.CreateFile("policy.rego", []byte(`
package docker

default allow = false

allow if input.image.repo == "example/allowlist"

deny_msg[msg] if {
	not allow
	msg := "repository required"
}
decision := {"allow": allow, "deny_msg": deny_msg}
`), 0600),
		fstest.CreateFile("policy_test.rego", []byte(`
package docker

test_missing_repo if {
	result := data.docker.decision with input as {"image": {}}
	result.allow
}

test_allowlisted_ok if {
	result := data.docker.decision with input as {"image": {"repo": "example/allowlist"}}
	result.allow
}
`), 0600),
	)

	cmd := buildxCmd(sb, withDir(dir), withArgs(
		"policy",
		"test",
		"--filename",
		"policy",
		".",
	))
	out, err := cmd.CombinedOutput()
	require.Error(t, err, string(out))
	require.Contains(t, string(out), "test_missing_repo: FAIL")
	require.Contains(t, string(out), "test_allowlisted_ok: PASS")
	require.Contains(t, string(out), "missing_input: input.image.repo")
}

func testPolicyTestNestedPath(t *testing.T, sb integration.Sandbox) {
	skipNoCompatBuildKit(t, sb, ">= 0.26.0-0", "policy input requires BuildKit v0.26.0+")
	dir := tmpdir(
		t,
		fstest.CreateFile("Dockerfile.rego", []byte(`
package docker

default allow = false

allow if input.image.repo == "example/allowlist"

decision := {"allow": allow}
`), 0600),
		fstest.CreateDir("scripts", 0700),
		fstest.CreateFile("scripts/policy_test.rego", []byte(`
package docker

test_allowlisted_repo if {
	result := data.docker.decision with input as {"image": {"repo": "example/allowlist"}}
	result.allow
}
`), 0600),
	)

	cmd := buildxCmd(sb, withDir(dir), withArgs(
		"policy",
		"test",
		"--filename",
		"Dockerfile",
		"scripts/policy_test.rego",
	))
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.Contains(t, string(out), "test_allowlisted_repo: PASS")

	cmd = buildxCmd(sb, withDir(dir), withArgs(
		"policy",
		"test",
		"--filename",
		"Dockerfile",
		"scripts",
	))
	out, err = cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.Contains(t, string(out), "test_allowlisted_repo: PASS")
}
