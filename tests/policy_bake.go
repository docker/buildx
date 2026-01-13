package tests

import (
	"testing"

	"github.com/containerd/continuity/fs/fstest"
	"github.com/moby/buildkit/util/testutil/integration"
	"github.com/stretchr/testify/require"
)

var policyBakeTests = []func(t *testing.T, sb integration.Sandbox){
	testBakePolicyConfigFlags,
}

func testBakePolicyConfigFlags(t *testing.T, sb integration.Sandbox) {
	skipNoCompatBuildKit(t, sb, ">= 0.26.0-0", "policy input requires BuildKit v0.26.0+")

	dockerfile := []byte("FROM scratch\n")
	defaultPolicy := []byte(`
package docker

default allow = false

allow if input.env.args["DEFAULT_OK"] == "1"

decision := {"allow": allow}
`)
	extraPolicy := []byte(`
package docker

default allow = false

allow if input.env.labels["com.example.extra"] == "1"

decision := {"allow": allow}
`)
	bakeFile := []byte(`
target "pass-both" {
  context = "."
  dockerfile = "Dockerfile"
  args = {
    DEFAULT_OK = "1"
  }
  labels = {
    "com.example.extra" = "1"
  }
  policy = [
    { filename = "extra.rego" },
  ]
  output = ["type=cacheonly"]
}

target "fail-default" {
  context = "."
  dockerfile = "Dockerfile"
  labels = {
    "com.example.extra" = "1"
  }
  policy = [
    { filename = "extra.rego" },
  ]
  output = ["type=cacheonly"]
}

target "fail-extra" {
  context = "."
  dockerfile = "Dockerfile"
  args = {
    DEFAULT_OK = "1"
  }
  policy = [
    { filename = "extra.rego" },
  ]
  output = ["type=cacheonly"]
}

target "reset-pass" {
  context = "."
  dockerfile = "Dockerfile"
  labels = {
    "com.example.extra" = "1"
  }
  policy = [
    { filename = "extra.rego", reset = true },
  ]
  output = ["type=cacheonly"]
}

target "reset-fail" {
  context = "."
  dockerfile = "Dockerfile"
  policy = [
    { filename = "extra.rego", reset = true },
  ]
  output = ["type=cacheonly"]
}

target "disabled" {
  context = "."
  dockerfile = "Dockerfile"
  policy = [
    { disabled = true },
  ]
  output = ["type=cacheonly"]
}

target "disabled-combined" {
  context = "."
  dockerfile = "Dockerfile"
  policy = [
    { filename = "extra.rego" },
    { disabled = true },
  ]
  output = ["type=cacheonly"]
}
`)

	dir := tmpdir(
		t,
		fstest.CreateFile("Dockerfile", dockerfile, 0600),
		fstest.CreateFile("Dockerfile.rego", defaultPolicy, 0600),
		fstest.CreateFile("extra.rego", extraPolicy, 0600),
		fstest.CreateFile("docker-bake.hcl", bakeFile, 0600),
	)

	cases := []struct {
		name            string
		target          string
		wantErrContains string
	}{
		{
			name:   "additional-policy-requires-default",
			target: "pass-both",
		},
		{
			name:            "additional-policy-missing-default",
			target:          "fail-default",
			wantErrContains: "not allowed by policy",
		},
		{
			name:            "additional-policy-missing-extra",
			target:          "fail-extra",
			wantErrContains: "not allowed by policy",
		},
		{
			name:   "reset-ignores-default",
			target: "reset-pass",
		},
		{
			name:            "reset-requires-extra",
			target:          "reset-fail",
			wantErrContains: "not allowed by policy",
		},
		{
			name:   "disabled-skips-default",
			target: "disabled",
		},
		{
			name:            "disabled-cannot-combine",
			target:          "disabled-combined",
			wantErrContains: "disabled policy cannot be combined with other policy flags",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := buildxCmd(sb, withDir(dir), withArgs(
				"bake",
				"--progress=plain",
				"--file", "docker-bake.hcl",
				tc.target,
			))
			out, err := cmd.CombinedOutput()
			if tc.wantErrContains == "" {
				require.NoError(t, err, string(out))
				return
			}
			require.Error(t, err, string(out))
			require.Contains(t, string(out), tc.wantErrContains)
		})
	}
}
