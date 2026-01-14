package tests

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/containerd/continuity/fs/fstest"
	"github.com/containerd/platforms"
	"github.com/distribution/reference"
	"github.com/docker/buildx/util/gitutil"
	"github.com/docker/buildx/util/gitutil/gittestutil"
	"github.com/moby/buildkit/identity"
	"github.com/moby/buildkit/util/contentutil"
	"github.com/moby/buildkit/util/testutil"
	"github.com/moby/buildkit/util/testutil/httpserver"
	"github.com/moby/buildkit/util/testutil/integration"
	digest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"
)

var policyBuildTests = []func(t *testing.T, sb integration.Sandbox){
	testBuildPolicyAllow,
	testBuildPolicyDeny,
	testBuildPolicyImageName,
	testBuildPolicyEnv,
	testBuildPolicyHTTP,
	testBuildPolicyGit,
	testBuildPolicyConfigFlags,
}

func testBuildPolicyAllow(t *testing.T, sb integration.Sandbox) {
	skipNoCompatBuildKit(t, sb, ">= 0.26.0-0", "policy input requires BuildKit v0.26.0+")
	dockerfile := []byte(`
FROM busybox:latest
RUN echo policy-ok
`)
	policyFile := []byte(`
package docker

default allow = false

allow if true

decision := {"allow": allow}
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
	skipNoCompatBuildKit(t, sb, ">= 0.26.0-0", "policy input requires BuildKit v0.26.0+")
	dockerfile := []byte(`
FROM busybox:latest
RUN echo policy-nope
`)
	policyFile := []byte(`
package docker

default allow = false

deny_msg := ["denied by test"]

decision := {"allow": allow, "deny_msg": deny_msg}
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
	require.Contains(t, string(out), "not allowed by policy")
	require.Contains(t, string(out), "loading policies "+policyPath)
	require.Contains(t, string(out), "policy decision for source")
	require.Contains(t, string(out), "DENY")
}

func testBuildPolicyImageName(t *testing.T, sb integration.Sandbox) {
	skipNoCompatBuildKit(t, sb, ">= 0.26.0-0", "policy input requires BuildKit v0.26.0+")
	registry, err := sb.NewRegistry()
	if errors.Is(err, integration.ErrRequirements) {
		t.Skip(err.Error())
	}
	require.NoError(t, err)

	baseRef := registry + "/buildx/policy-base:" + identity.NewID()
	baseDir := tmpdir(
		t,
		fstest.CreateFile("Dockerfile", []byte("FROM busybox:latest\nLABEL com.example.policy=label\nENV POLICY_ENV=envval\n"), 0600),
	)
	baseCmd := buildxCmd(sb, withDir(baseDir), withArgs(
		"build",
		"--progress=plain",
		"--output=type=image,name="+baseRef+",push=true",
		baseDir,
	))
	baseOut, err := baseCmd.CombinedOutput()
	require.NoError(t, err, string(baseOut))

	baseDesc, provider, err := contentutil.ProviderFromRef(baseRef)
	require.NoError(t, err)
	_, err = testutil.ReadImages(sb.Context(), provider, baseDesc)
	require.NoError(t, err)
	baseCanonicalRef := baseRef + "@" + baseDesc.Digest.String()

	parsedBase, err := reference.ParseNormalizedNamed(baseRef)
	require.NoError(t, err)
	baseFullRepo := parsedBase.Name()

	platformStr := platforms.Format(platforms.Normalize(platforms.DefaultSpec()))

	testCases := []struct {
		name            string
		policy          string
		dockerfileName  string
		dockerfileBody  string
		buildArgs       []string
		wantErrContains string
	}{
		{
			name: "repo-allow-busybox",
			policy: `
package docker

default allow = false

allow if not input.image

allow if input.image.repo == "busybox"

decision := {"allow": allow}
`,
			dockerfileName: "Dockerfile",
			dockerfileBody: "FROM busybox:latest\nRUN echo repo-ok\n",
		},
		{
			name: "repo-deny-alpine",
			policy: `
package docker

default allow = false

allow if not input.image

allow if input.image.repo == "busybox"

decision := {"allow": allow}
`,
			dockerfileName:  "Dockerfile",
			dockerfileBody:  "FROM alpine:latest\nRUN echo repo-deny\n",
			wantErrContains: "not allowed by policy",
		},
		{
			name: "tag-allow-alpine-latest",
			policy: `
package docker

default allow = false

allow if not input.image

allow if input.image.tag == "latest"

decision := {"allow": allow}
`,
			dockerfileName: "Dockerfile",
			dockerfileBody: "FROM alpine:latest\nRUN echo tag-ok\n",
		},
		{
			name: "tag-deny-busybox-latest",
			policy: `
package docker

default allow = false

allow if not input.image

allow if input.image.tag == "stable"

decision := {"allow": allow}
`,
			dockerfileName:  "Dockerfile",
			dockerfileBody:  "FROM busybox:latest\nRUN echo tag-deny\n",
			wantErrContains: "not allowed by policy",
		},
		{
			name: "host-allow-docker-io",
			policy: `
package docker

default allow = false

allow if not input.image

allow if input.image.host == "docker.io"

decision := {"allow": allow}
`,
			dockerfileName: "Dockerfile",
			dockerfileBody: "FROM busybox:latest\nRUN echo host-ok\n",
		},
		{
			name: "host-deny-local-registry",
			policy: `
package docker

default allow = false

allow if not input.image

allow if input.image.host == "docker.io"

decision := {"allow": allow}
`,
			dockerfileName:  "Dockerfile",
			dockerfileBody:  fmt.Sprintf("FROM %s\nRUN echo host-deny\n", baseRef),
			wantErrContains: "not allowed by policy",
		},
		{
			name: "full-repo-allow-library-busybox",
			policy: fmt.Sprintf(`
package docker

default allow = false

allow if not input.image

allow if input.image.fullRepo == "%s"

decision := {"allow": allow}
`, baseFullRepo),
			dockerfileName: "Dockerfile",
			dockerfileBody: fmt.Sprintf("FROM %s\nRUN echo full-repo-ok\n", baseRef),
		},
		{
			name: "full-repo-deny-alpine",
			policy: fmt.Sprintf(`
package docker

default allow = false

allow if not input.image

allow if input.image.fullRepo == "%s"

decision := {"allow": allow}
`, baseFullRepo),
			dockerfileName:  "Dockerfile",
			dockerfileBody:  "FROM alpine:latest\nRUN echo full-repo-deny\n",
			wantErrContains: "not allowed by policy",
		},
		{
			name: "platform-allow-default",
			policy: fmt.Sprintf(`
package docker

default allow = false

allow if not input.image

allow if input.image.platform == "%s"

decision := {"allow": allow}
`, platformStr),
			dockerfileName: "Dockerfile",
			dockerfileBody: "FROM busybox:latest\nRUN echo platform-ok\n",
		},
		{
			name: "canonical-allow",
			policy: `
package docker

default allow = false

allow if not input.image

allow if input.image.isCanonical

			decision := {"allow": allow}
`,
			dockerfileName: "Dockerfile",
			dockerfileBody: fmt.Sprintf("FROM %s\nRUN echo canonical-ok\n", baseCanonicalRef),
		},
		{
			name: "canonical-deny",
			policy: `
package docker

default allow = false

allow if not input.image

allow if input.image.isCanonical

			decision := {"allow": allow}
`,
			dockerfileName:  "Dockerfile",
			dockerfileBody:  fmt.Sprintf("FROM %s\nRUN echo canonical-deny\n", baseRef),
			wantErrContains: "not allowed by policy",
		},
		{
			name: "checksum-allow",
			policy: fmt.Sprintf(`
package docker

default allow = false

allow if not input.image

allow if input.image.checksum == "%s"

decision := {"allow": allow}
`, baseDesc.Digest.String()),
			dockerfileName: "Dockerfile",
			dockerfileBody: fmt.Sprintf("FROM %s\nRUN echo checksum-ok\n", baseCanonicalRef),
		},
		{
			name: "checksum-deny",
			policy: `
package docker

default allow = false

allow if not input.image

allow if input.image.checksum == "sha256:0000000000000000000000000000000000000000000000000000000000000000"

decision := {"allow": allow}
`,
			dockerfileName:  "Dockerfile",
			dockerfileBody:  fmt.Sprintf("FROM %s\nRUN echo checksum-deny\n", baseCanonicalRef),
			wantErrContains: "not allowed by policy",
		},
		{
			name: "config-label-allow",
			policy: `
package docker

default allow = false

allow if not input.image

allow if input.image.labels["com.example.policy"] == "label"

decision := {"allow": allow}
`,
			dockerfileName: "Dockerfile",
			dockerfileBody: fmt.Sprintf("FROM %s\nRUN echo label-ok\n", baseRef),
		},
		{
			name: "config-env-allow",
			policy: `
package docker

default allow = false

allow if not input.image

allow if input.image.env[_] == "POLICY_ENV=envval"

decision := {"allow": allow}
`,
			dockerfileName: "Dockerfile",
			dockerfileBody: fmt.Sprintf("FROM %s\nRUN echo env-ok\n", baseRef),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			buildDir := tmpdir(
				t,
				fstest.CreateFile(tc.dockerfileName, []byte(tc.dockerfileBody), 0600),
				fstest.CreateFile("policy.rego", []byte(tc.policy), 0600),
			)
			policyPath := filepath.Join(buildDir, "policy.rego")

			args := []string{
				"build",
				"--progress=plain",
				"--policy", "filename=" + policyPath,
				"--output=type=cacheonly",
			}
			args = append(args, tc.buildArgs...)
			args = append(args, buildDir)

			cmd := buildxCmd(sb, withDir(buildDir), withArgs(
				args...,
			))
			out, err := cmd.CombinedOutput()
			if tc.wantErrContains == "" {
				require.NoError(t, err, string(out))
				require.Contains(t, string(out), "loading policies "+policyPath)
			} else {
				require.Error(t, err, string(out))
				require.Contains(t, string(out), tc.wantErrContains)
			}
		})
	}
}

func testBuildPolicyEnv(t *testing.T, sb integration.Sandbox) {
	skipNoCompatBuildKit(t, sb, ">= 0.26.0-0", "policy input requires BuildKit v0.26.0+")
	testCases := []struct {
		name            string
		policy          string
		dockerfileName  string
		dockerfileBody  string
		buildArgs       []string
		wantErrContains string
	}{
		{
			name: "env-arg-allow",
			policy: `
package docker

default allow = false

allow if input.env.args["POLICY_CASE"] == "arg"

decision := {"allow": allow}
`,
			dockerfileName: "Dockerfile",
			dockerfileBody: "FROM busybox:latest\nRUN echo env-arg-ok\n",
			buildArgs:      []string{"--build-arg", "POLICY_CASE=arg"},
		},
		{
			name: "env-arg-deny",
			policy: `
package docker

default allow = false

allow if input.env.args["POLICY_CASE"] == "arg"

decision := {"allow": allow}
`,
			dockerfileName:  "Dockerfile",
			dockerfileBody:  "FROM busybox:latest\nRUN echo env-arg-deny\n",
			wantErrContains: "not allowed by policy",
		},
		{
			name: "env-label-allow",
			policy: `
package docker

default allow = false

allow if input.env.labels["com.example.policy"] == "label"

decision := {"allow": allow}
`,
			dockerfileName: "Dockerfile",
			dockerfileBody: "FROM busybox:latest\nRUN echo env-label-ok\n",
			buildArgs:      []string{"--label", "com.example.policy=label"},
		},
		{
			name: "env-target-deny",
			policy: `
package docker

default allow = false

allow if input.env.target == "final"

decision := {"allow": allow}
`,
			dockerfileName:  "Dockerfile",
			dockerfileBody:  "FROM busybox:latest AS build\nRUN echo stage-build\n\nFROM busybox:latest AS final\nRUN echo stage-final\n",
			buildArgs:       []string{"--target", "build"},
			wantErrContains: "not allowed by policy",
		},
		{
			name: "env-filename-allow",
			policy: `
package docker

default allow = false

allow if input.env.filename == "Dockerfile.custom"

decision := {"allow": allow}
`,
			dockerfileName: "Dockerfile.custom",
			dockerfileBody: "FROM busybox:latest\nRUN echo filename-ok\n",
			buildArgs:      []string{"-f", "Dockerfile.custom"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			buildDir := tmpdir(
				t,
				fstest.CreateFile(tc.dockerfileName, []byte(tc.dockerfileBody), 0600),
				fstest.CreateFile("policy.rego", []byte(tc.policy), 0600),
			)
			policyPath := filepath.Join(buildDir, "policy.rego")

			args := []string{
				"build",
				"--progress=plain",
				"--policy", "filename=" + policyPath,
				"--output=type=cacheonly",
			}
			args = append(args, tc.buildArgs...)
			args = append(args, buildDir)

			cmd := buildxCmd(sb, withDir(buildDir), withArgs(
				args...,
			))
			out, err := cmd.CombinedOutput()
			if tc.wantErrContains == "" {
				require.NoError(t, err, string(out))
				require.Contains(t, string(out), "loading policies "+policyPath)
			} else {
				require.Error(t, err, string(out))
				require.Contains(t, string(out), tc.wantErrContains)
			}
		})
	}
}

func testBuildPolicyHTTP(t *testing.T, sb integration.Sandbox) {
	skipNoCompatBuildKit(t, sb, ">= 0.26.0-0", "policy input requires BuildKit v0.26.0+")
	resp := &httpserver.Response{Content: []byte("policy-http")}
	server := httpserver.NewTestServer(map[string]*httpserver.Response{
		"/file": resp,
	})
	defer server.Close()

	parsedURL, err := url.Parse(server.URL)
	require.NoError(t, err)

	baseURL := server.URL + "/file"
	queryURL := baseURL + "?policy=allow&case=http"
	checksum := digest.FromBytes(resp.Content).String()
	testCases := []struct {
		name                 string
		policy               string
		addURL               string
		wantErrContains      string
		requiresHTTPChecksum bool
	}{
		{
			name: "http-url-allow",
			policy: fmt.Sprintf(`
package docker

default allow = false

allow if not input.http

allow if input.http.url == "%s"

decision := {"allow": allow}
`, queryURL),
			addURL: queryURL,
		},
		{
			name: "http-schema-allow",
			policy: `
package docker

default allow = false

allow if not input.http

allow if input.http.schema == "http"

decision := {"allow": allow}
`,
			addURL: baseURL,
		},
		{
			name: "http-host-allow",
			policy: fmt.Sprintf(`
package docker

default allow = false

allow if not input.http

allow if input.http.host == "%s"

decision := {"allow": allow}
`, parsedURL.Host),
			addURL: baseURL,
		},
		{
			name: "http-path-allow",
			policy: `
package docker

default allow = false

allow if not input.http

allow if input.http.path == "/file"

decision := {"allow": allow}
`,
			addURL: baseURL,
		},
		{
			name: "http-query-allow",
			policy: `
package docker

default allow = false

allow if not input.http

allow if input.http.query["policy"][_] == "allow"

decision := {"allow": allow}
`,
			addURL: queryURL,
		},
		{
			name: "http-checksum-allow",
			policy: fmt.Sprintf(`
package docker

default allow = false

allow if not input.http

allow if input.http.checksum == "%s"

decision := {"allow": allow}
`, checksum),
			addURL:               baseURL,
			requiresHTTPChecksum: true,
		},
		{
			name: "http-checksum-deny",
			policy: `
package docker

default allow = false

allow if not input.http

allow if input.http.checksum == "sha256:0000000000000000000000000000000000000000000000000000000000000000"

decision := {"allow": allow}
`,
			addURL:               baseURL,
			wantErrContains:      "not allowed by policy",
			requiresHTTPChecksum: true,
		},
		{
			name: "http-host-deny",
			policy: `
package docker

default allow = false

allow if not input.http

allow if input.http.host == "example.invalid"

decision := {"allow": allow}
`,
			addURL:          baseURL,
			wantErrContains: "not allowed by policy",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.requiresHTTPChecksum {
				sbDriver, _, _ := driverName(sb.Name())
				if sbDriver != "remote" {
					t.Skip("http checksum policy input requires remote driver")
				}
				skipNoCompatBuildKit(t, sb, ">= 0.26.3-0", "http checksum policy input")
			}
			dockerfile := fmt.Appendf(nil, "FROM busybox:latest\nADD %s /tmp/file\n", tc.addURL)
			dir := tmpdir(
				t,
				fstest.CreateFile("Dockerfile", dockerfile, 0600),
				fstest.CreateFile("policy.rego", []byte(tc.policy), 0600),
			)
			policyPath := filepath.Join(dir, "policy.rego")
			policyArg := "filename=" + policyPath
			if tc.name == "git-schema-allow" {
				policyArg = "log-level=debug," + policyArg
			}

			cmd := buildxCmd(sb, withDir(dir), withArgs(
				"build",
				"--progress=plain",
				"--policy", policyArg,
				"--output=type=cacheonly",
				dir,
			))
			out, err := cmd.CombinedOutput()
			if tc.wantErrContains == "" {
				require.NoError(t, err, string(out))
				require.Contains(t, string(out), "loading policies "+policyPath)
			} else {
				require.Error(t, err, string(out))
				require.Contains(t, string(out), tc.wantErrContains)
			}
		})
	}
}

func testBuildPolicyGit(t *testing.T, sb integration.Sandbox) {
	skipNoCompatBuildKit(t, sb, ">= 0.26.0-0", "policy input requires BuildKit v0.26.0+")

	gitDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(gitDir, "Dockerfile"), []byte("FROM busybox:latest\nRUN echo git\n"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(gitDir, "a"), []byte("a"), 0600))

	git, err := gitutil.New(gitutil.WithWorkingDir(gitDir))
	require.NoError(t, err)

	gittestutil.GitInit(git, t)
	gittestutil.GitAdd(git, t, "Dockerfile", "a")
	gittestutil.GitCommit(git, t, "initial commit")

	gittestutil.GitTagAnnotated(git, t, "v0.1", "v0.1release")

	require.NoError(t, os.WriteFile(filepath.Join(gitDir, "b"), []byte("b"), 0600))
	gittestutil.GitAdd(git, t, "b")
	gittestutil.GitCommit(git, t, "b")
	_, err = git.Run("checkout", "-B", "v2")
	require.NoError(t, err)

	commitHead, err := git.Run("rev-parse", "HEAD")
	require.NoError(t, err)
	commitTag, err := git.Run("rev-parse", "v0.1")
	require.NoError(t, err)
	commitTagCommit, err := git.Run("rev-parse", "v0.1^{commit}")
	require.NoError(t, err)
	baseURL := gittestutil.GitServeHTTP(git, t)
	tagURL := baseURL + "#v0.1"
	branchURL := baseURL + "#v2"
	parsedURL, err := url.Parse(baseURL)
	require.NoError(t, err)

	testCases := []struct {
		name               string
		policy             string
		context            string
		wantErrContains    string
		requiresGitResolve bool
	}{
		{
			name: "git-schema-allow",
			policy: `
package docker

default allow = false

allow if not input.git

allow if input.git.schema != ""

decision := {"allow": allow}
`,
			context: baseURL,
		},
		{
			name: "git-host-allow",
			policy: fmt.Sprintf(`
package docker

default allow = false

allow if not input.git

allow if input.git.host == "%s"

decision := {"allow": allow}
`, parsedURL.Host),
			context: baseURL,
		},
		{
			name: "git-remote-allow",
			policy: fmt.Sprintf(`
package docker

default allow = false

allow if not input.git

allow if endswith(input.git.remote, "%s")

decision := {"allow": allow}
`, parsedURL.Path),
			context: baseURL,
		},
		{
			name: "git-ref-tag-allow",
			policy: `
package docker

default allow = false

allow if not input.git

allow if input.git.ref == "refs/tags/v0.1"

decision := {"allow": allow}
`,
			context:            tagURL,
			requiresGitResolve: true,
		},
		{
			name: "git-branch-allow",
			policy: `
package docker

default allow = false

allow if not input.git

allow if input.git.branch == "v2"

decision := {"allow": allow}
`,
			context:            branchURL,
			requiresGitResolve: true,
		},
		{
			name: "git-tagname-allow",
			policy: `
package docker

default allow = false

allow if not input.git

allow if input.git.tagName == "v0.1"

decision := {"allow": allow}
`,
			context:            tagURL,
			requiresGitResolve: true,
		},
		{
			name: "git-checksum-allow",
			policy: fmt.Sprintf(`
package docker

default allow = false

allow if not input.git

allow if input.git.checksum == "%s"

decision := {"allow": allow}
`, commitTag),
			context:            tagURL,
			requiresGitResolve: true,
		},
		{
			name: "git-commit-checksum-allow",
			policy: fmt.Sprintf(`
package docker

default allow = false

allow if not input.git

allow if input.git.commitChecksum == "%s"

decision := {"allow": allow}
`, commitTagCommit),
			context:            tagURL,
			requiresGitResolve: true,
		},
		{
			name: "git-annotated-tag-allow",
			policy: `
package docker

default allow = false

allow if not input.git

allow if input.git.isAnnotatedTag == true

decision := {"allow": allow}
`,
			context:            tagURL,
			requiresGitResolve: true,
		},
		{
			name: "git-commit-message-allow",
			policy: `
package docker

default allow = false

allow if not input.git

allow if input.git.commit.message == "initial commit"

decision := {"allow": allow}
`,
			context:            tagURL,
			requiresGitResolve: true,
		},
		{
			name: "git-tag-object-allow",
			policy: `
package docker

default allow = false

allow if not input.git

allow if input.git.tag.tag == "v0.1"

decision := {"allow": allow}
`,
			context:            tagURL,
			requiresGitResolve: true,
		},
		{
			name: "git-checksum-deny",
			policy: `
package docker

default allow = false

allow if not input.git

allow if input.git.checksum == "deadbeef"

decision := {"allow": allow}
`,
			context:            tagURL,
			wantErrContains:    "not allowed by policy",
			requiresGitResolve: true,
		},
		{
			name: "git-commit-ref-allow",
			policy: `
package docker

default allow = false

allow if not input.git

allow if input.git.isCommitRef == true

decision := {"allow": allow}
`,
			context:            baseURL + "#" + commitHead,
			requiresGitResolve: true,
		},
		{
			name: "git-host-deny",
			policy: `
package docker

default allow = false

allow if not input.git

allow if input.git.host == "example.invalid"

decision := {"allow": allow}
`,
			context:         tagURL,
			wantErrContains: "not allowed by policy",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.requiresGitResolve {
				sbDriver, _, _ := driverName(sb.Name())
				if sbDriver != "remote" {
					t.Skip("git policy metadata requires remote driver")
				}
			}
			dir := tmpdir(
				t,
				fstest.CreateFile("policy.rego", []byte(tc.policy), 0600),
			)
			policyPath := filepath.Join(dir, "policy.rego")

			cmd := buildxCmd(sb, withDir(dir), withArgs(
				"build",
				"--progress=plain",
				"--policy", "filename="+policyPath,
				"--output=type=cacheonly",
				tc.context,
			))
			out, err := cmd.CombinedOutput()
			if tc.wantErrContains == "" {
				require.NoError(t, err, string(out))
				require.Contains(t, string(out), "loading policies "+policyPath)
			} else {
				require.Error(t, err, string(out))
				require.Contains(t, string(out), tc.wantErrContains)
			}
		})
	}
}

func testBuildPolicyConfigFlags(t *testing.T, sb integration.Sandbox) {
	skipNoCompatBuildKit(t, sb, ">= 0.26.0-0", "policy input requires BuildKit v0.26.0+")

	dockerfile := []byte("FROM busybox:latest\nRUN echo policy-flags\n")
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
	denyPolicy := []byte(`
package docker

default allow = false

decision := {"allow": allow}
`)

	t.Run("additional-policy-requires-default", func(t *testing.T) {
		dir := tmpdir(
			t,
			fstest.CreateFile("Dockerfile", dockerfile, 0600),
			fstest.CreateFile("Dockerfile.rego", defaultPolicy, 0600),
			fstest.CreateFile("extra.rego", extraPolicy, 0600),
		)
		extraPath := filepath.Join(dir, "extra.rego")

		cmd := buildxCmd(sb, withDir(dir), withArgs(
			"build",
			"--progress=plain",
			"--policy", "filename="+extraPath,
			"--build-arg", "DEFAULT_OK=1",
			"--label", "com.example.extra=1",
			"--output=type=cacheonly",
			dir,
		))
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))

		cmd = buildxCmd(sb, withDir(dir), withArgs(
			"build",
			"--progress=plain",
			"--policy", "filename="+extraPath,
			"--label", "com.example.extra=1",
			"--output=type=cacheonly",
			dir,
		))
		out, err = cmd.CombinedOutput()
		require.Error(t, err, string(out))
		require.Contains(t, string(out), "not allowed by policy")

		cmd = buildxCmd(sb, withDir(dir), withArgs(
			"build",
			"--progress=plain",
			"--policy", "filename="+extraPath,
			"--build-arg", "DEFAULT_OK=1",
			"--output=type=cacheonly",
			dir,
		))
		out, err = cmd.CombinedOutput()
		require.Error(t, err, string(out))
		require.Contains(t, string(out), "not allowed by policy")
	})

	t.Run("reset-ignores-default", func(t *testing.T) {
		dir := tmpdir(
			t,
			fstest.CreateFile("Dockerfile", dockerfile, 0600),
			fstest.CreateFile("Dockerfile.rego", defaultPolicy, 0600),
			fstest.CreateFile("extra.rego", extraPolicy, 0600),
		)
		extraPath := filepath.Join(dir, "extra.rego")

		cmd := buildxCmd(sb, withDir(dir), withArgs(
			"build",
			"--progress=plain",
			"--policy", "reset=true,filename="+extraPath,
			"--label", "com.example.extra=1",
			"--output=type=cacheonly",
			dir,
		))
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))

		cmd = buildxCmd(sb, withDir(dir), withArgs(
			"build",
			"--progress=plain",
			"--policy", "reset=true,filename="+extraPath,
			"--output=type=cacheonly",
			dir,
		))
		out, err = cmd.CombinedOutput()
		require.Error(t, err, string(out))
		require.Contains(t, string(out), "not allowed by policy")
	})

	t.Run("disabled-skips-default", func(t *testing.T) {
		dir := tmpdir(
			t,
			fstest.CreateFile("Dockerfile", dockerfile, 0600),
			fstest.CreateFile("Dockerfile.rego", denyPolicy, 0600),
		)

		cmd := buildxCmd(sb, withDir(dir), withArgs(
			"build",
			"--progress=plain",
			"--policy", "disabled=true",
			"--output=type=cacheonly",
			dir,
		))
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
	})

	t.Run("disabled-cannot-combine-with-extra", func(t *testing.T) {
		dir := tmpdir(
			t,
			fstest.CreateFile("Dockerfile", dockerfile, 0600),
			fstest.CreateFile("extra.rego", denyPolicy, 0600),
		)
		extraPath := filepath.Join(dir, "extra.rego")

		cmd := buildxCmd(sb, withDir(dir), withArgs(
			"build",
			"--progress=plain",
			"--policy", "filename="+extraPath,
			"--policy", "disabled=true",
			"--output=type=cacheonly",
			dir,
		))
		out, err := cmd.CombinedOutput()
		require.Error(t, err, string(out))
		require.Contains(t, string(out), "disabled policy cannot be combined with other policy flags")
	})
}
