package tests

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/containerd/continuity/fs/fstest"
	"github.com/docker/buildx/util/gitutil"
	"github.com/docker/buildx/util/gitutil/gittestutil"
	"github.com/moby/buildkit/identity"
	bkgitutil "github.com/moby/buildkit/util/gitutil"
	"github.com/moby/buildkit/util/testutil/integration"
	"github.com/stretchr/testify/require"
)

var policyBakeTests = []func(t *testing.T, sb integration.Sandbox){
	testBakePolicyConfigFlags,
	testBakeRemoteGitNoPolicyWithProxyNetwork,
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
		policyArgs      []string
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
		{
			name:       "global-disabled-skips-target-policies",
			target:     "fail-extra",
			policyArgs: []string{"--policy", "disabled=true"},
		},
		{
			name:            "global-policy-rejects-filename",
			target:          "pass-both",
			policyArgs:      []string{"--policy", "filename=extra.rego"},
			wantErrContains: "--policy does not accept filename",
		},
		{
			name:            "global-policy-rejects-reset",
			target:          "pass-both",
			policyArgs:      []string{"--policy", "reset=true"},
			wantErrContains: "--policy does not accept reset",
		},
		{
			name:            "global-disabled-cannot-combine",
			target:          "pass-both",
			policyArgs:      []string{"--policy", "disabled=true,strict=true"},
			wantErrContains: "disabled policy cannot be combined with other policy flags",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := []string{
				"bake",
				"--progress=plain",
				"--file", "docker-bake.hcl",
			}
			args = append(args, tc.policyArgs...)
			args = append(args, tc.target)
			cmd := buildxCmd(sb, withDir(dir), withArgs(
				args...,
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

func testBakeRemoteGitNoPolicyWithProxyNetwork(t *testing.T, sb integration.Sandbox) {
	if !isDockerContainerWorker(sb) {
		t.Skip("only testing with docker-container worker")
	}
	skipNoCompatBuildKit(t, sb, ">= 0.31.0-0", "network proxy requires BuildKit v0.31.0+")

	dockerfile := []byte(`
FROM alpine:latest
RUN wget -qO- https://checkip.amazonaws.com/ | grep -Eq "^[0-9a-fA-F:.]+$"
`)
	bakeFile := []byte(`
target "default" {
  output = ["type=cacheonly"]
}
`)
	dir := tmpdir(
		t,
		fstest.CreateFile("Dockerfile", dockerfile, 0600),
		fstest.CreateFile("docker-bake.hcl", bakeFile, 0600),
	)

	git, err := gitutil.New(bkgitutil.WithDir(dir))
	require.NoError(t, err)
	gittestutil.GitInit(git, t)
	gittestutil.GitAdd(git, t, "Dockerfile", "docker-bake.hcl")
	gittestutil.GitCommit(git, t, "initial commit")
	addr := gittestutil.GitServeHTTP(git, t)

	buildkitdConfPath := filepath.Join(t.TempDir(), "buildkitd.toml")
	require.NoError(t, os.WriteFile(buildkitdConfPath, []byte("proxyNetwork = true\n"), 0600))

	builderName := "proxy-network-" + identity.NewID()
	var created bool
	t.Cleanup(func() {
		if !created {
			return
		}
		out, err := rmCmd(sb, withArgs("-f", builderName))
		require.NoError(t, err, out)
	})

	out, err := createCmd(sb, withArgs(
		"--bootstrap",
		"--name="+builderName,
		"--driver=docker-container",
		"--driver-opt", "network=host",
		"--driver-opt", "image="+buildkitImage,
		"--buildkitd-config="+buildkitdConfPath,
	))
	require.NoError(t, err, out)
	created = true

	cmd := buildxCmd(sb, withDir(t.TempDir()), withArgs(
		"bake",
		"--progress=plain",
		"--no-cache",
		"--builder", builderName,
		addr,
	))
	outBytes, err := cmd.CombinedOutput()
	out = string(outBytes)
	require.NoError(t, err, out)
	require.Contains(t, out, "proxy network requests:")
	require.Contains(t, out, "GET https://checkip.amazonaws.com/ -> 200")
}
