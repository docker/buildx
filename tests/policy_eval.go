package tests

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	urlpkg "net/url"
	"strings"
	"testing"

	"github.com/containerd/continuity/fs/fstest"
	"github.com/docker/buildx/policy"
	"github.com/moby/buildkit/identity"
	"github.com/moby/buildkit/util/testutil/httpserver"
	"github.com/moby/buildkit/util/testutil/integration"
	digest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"
)

var policyEvalTests = []func(t *testing.T, sb integration.Sandbox){
	testPolicyEvalAllow,
	testPolicyEvalDeny,
	testPolicyEvalPrint,
	testPolicyEvalFields,
	testPolicyEvalLabel,
	testPolicyEvalProvenance,
	testPolicyEvalProvenanceValidation,
	testPolicyEvalHTTP,
}

func testPolicyEvalAllow(t *testing.T, sb integration.Sandbox) {
	skipNoCompatBuildKit(t, sb, ">= 0.26.0-0", "policy input requires BuildKit v0.26.0+")
	policyFile := []byte(`
package docker

default allow = false

allow if not input.image

allow if input.image.repo == "busybox"

decision := {"allow": allow}
`)
	dir := tmpdir(
		t,
		fstest.CreateFile("policy.rego", policyFile, 0600),
	)

	cmd := buildxCmd(sb, withDir(dir), withArgs(
		"policy",
		"eval",
		"--filename",
		"policy",
		"docker-image://busybox:latest",
	))
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}

func testPolicyEvalDeny(t *testing.T, sb integration.Sandbox) {
	skipNoCompatBuildKit(t, sb, ">= 0.26.0-0", "policy input requires BuildKit v0.26.0+")
	policyFile := []byte(`
package docker

default allow = false

allow if not input.image

allow if input.image.repo == "alpine"

decision := {"allow": allow}
`)
	dir := tmpdir(
		t,
		fstest.CreateFile("policy.rego", policyFile, 0600),
	)

	cmd := buildxCmd(sb, withDir(dir), withArgs(
		"policy",
		"eval",
		"--filename",
		"policy",
		"docker-image://busybox:latest",
	))
	out, err := cmd.CombinedOutput()
	require.Error(t, err, string(out))
	require.Contains(t, string(out), "policy denied")
}

func testPolicyEvalPrint(t *testing.T, sb integration.Sandbox) {
	skipNoCompatBuildKit(t, sb, ">= 0.26.0-0", "policy input requires BuildKit v0.26.0+")
	cmd := buildxCmd(sb, withArgs(
		"policy",
		"eval",
		"--print",
		"docker-image://busybox:latest",
	))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	require.NoError(t, err, stderr.String())

	var input policy.Input
	err = json.Unmarshal(out, &input)
	require.NoError(t, err, string(out))
	require.NotNil(t, input.Image)
	require.Equal(t, "busybox", input.Image.Repo)
}

func testPolicyEvalFields(t *testing.T, sb integration.Sandbox) {
	skipNoCompatBuildKit(t, sb, ">= 0.26.0-0", "policy input requires BuildKit v0.26.0+")
	registry, err := sb.NewRegistry()
	if errors.Is(err, integration.ErrRequirements) {
		t.Skip(err.Error())
	}
	require.NoError(t, err)
	unlabeledRef := registry + "/buildx/policy-eval-fields:" + identity.NewID()
	labeledRef := registry + "/buildx/policy-eval-fields:" + identity.NewID()

	dir := tmpdir(
		t,
		fstest.CreateFile("Dockerfile", []byte("FROM busybox:latest\n"), 0600),
	)
	buildCmd := buildxCmd(sb, withDir(dir), withArgs(
		"build",
		"--progress=plain",
		"--output=type=image,name="+unlabeledRef+",push=true",
		dir,
	))
	buildOut, err := buildCmd.CombinedOutput()
	require.NoError(t, err, string(buildOut))

	labeledDir := tmpdir(
		t,
		fstest.CreateFile("Dockerfile", []byte("FROM busybox:latest\nLABEL com.example.policy=label\n"), 0600),
	)
	labeledCmd := buildxCmd(sb, withDir(labeledDir), withArgs(
		"build",
		"--progress=plain",
		"--output=type=image,name="+labeledRef+",push=true",
		labeledDir,
	))
	labeledOut, err := labeledCmd.CombinedOutput()
	require.NoError(t, err, string(labeledOut))

	cmd := buildxCmd(sb, withArgs(
		"policy",
		"eval",
		"--print",
		"--fields",
		"image.labels",
		fmt.Sprintf("docker-image://%s", unlabeledRef),
	))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	require.NoError(t, err, stderr.String())

	var input policy.Input
	err = json.Unmarshal(out, &input)
	require.NoError(t, err, string(out))
	require.NotNil(t, input.Image)
	require.Empty(t, input.Image.Labels)

	cmd = buildxCmd(sb, withArgs(
		"policy",
		"eval",
		"--print",
		"--fields",
		"image.labels",
		fmt.Sprintf("docker-image://%s", labeledRef),
	))
	stderr.Reset()
	out, err = cmd.Output()
	require.NoError(t, err, stderr.String())

	err = json.Unmarshal(out, &input)
	require.NoError(t, err, string(out))
	require.NotNil(t, input.Image)
	require.Equal(t, "label", input.Image.Labels["com.example.policy"])
}

func testPolicyEvalLabel(t *testing.T, sb integration.Sandbox) {
	skipNoCompatBuildKit(t, sb, ">= 0.26.0-0", "policy input requires BuildKit v0.26.0+")
	registry, err := sb.NewRegistry()
	if errors.Is(err, integration.ErrRequirements) {
		t.Skip(err.Error())
	}
	require.NoError(t, err)
	imageRef := registry + "/buildx/policy-eval-label:" + identity.NewID()

	dir := tmpdir(
		t,
		fstest.CreateFile("Dockerfile", []byte("FROM busybox:latest\nLABEL com.example.policy=label\n"), 0600),
		fstest.CreateFile("policy.rego", []byte(`
package docker

default allow = false

allow if input.image.labels["com.example.policy"] == "label"

decision := {"allow": allow}
`), 0600),
	)
	buildCmd := buildxCmd(sb, withDir(dir), withArgs(
		"build",
		"--progress=plain",
		"--output=type=image,name="+imageRef+",push=true",
		dir,
	))
	buildOut, err := buildCmd.CombinedOutput()
	require.NoError(t, err, string(buildOut))

	cmd := buildxCmd(sb, withDir(dir), withArgs(
		"policy",
		"eval",
		"--filename",
		"policy",
		fmt.Sprintf("docker-image://%s", imageRef),
	))
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}

func testPolicyEvalProvenance(t *testing.T, sb integration.Sandbox) {
	// TODO: update after v0.28+ to test with non-master BuildKit
	if buildkitTag() != "master" {
		t.Skip("policy eval provenance integration requires TEST_BUILDKIT_TAG=master")
	}
	if !isDockerContainerWorker(sb) {
		t.Skip("policy eval provenance integration requires docker-container worker")
	}
	// Base policy input support.
	skipNoCompatBuildKit(t, sb, ">= 0.26.0-0", "policy input requires BuildKit v0.26.0+")

	imageRef := createPolicyEvalProvenanceImage(t, sb)

	cmd := buildxCmd(sb, withArgs(
		"policy",
		"eval",
		"--print",
		"--fields",
		"image.provenance",
		"docker-image://"+imageRef,
	))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	expectResolveAttestations := true
	t.Logf("buildkit image=%s worker=%s expectResolveAttestations=%v", buildkitImage, sb.Name(), expectResolveAttestations)
	if err != nil && strings.Contains(stderr.String(), "maximum attempts reached for resolving policy metadata") {
		if expectResolveAttestations {
			require.NoError(t, err, stderr.String())
		}
		t.Skip("policy eval provenance requires BuildKit ResolveAttestations support (currently master-only)")
	}
	require.NoError(t, err, stderr.String())

	var input policy.Input
	err = json.Unmarshal(out, &input)
	require.NoError(t, err, string(out))
	require.NotNil(t, input.Image)
	require.True(t, input.Image.HasProvenance, "expected source image to report provenance")
	if input.Image.Provenance == nil {
		if expectResolveAttestations {
			require.NotNil(t, input.Image.Provenance, "expected image provenance to be resolved with master BuildKit")
		}
		t.Skip("policy eval provenance requires BuildKit ResolveAttestations support (currently master-only)")
	}
	require.Equal(t, "https://slsa.dev/provenance/v1", input.Image.Provenance.PredicateType)
	require.Equal(t, "dockerfile.v0", input.Image.Provenance.Frontend)
}

func testPolicyEvalProvenanceValidation(t *testing.T, sb integration.Sandbox) {
	// TODO: update after v0.28+ is released to test with non-master BuildKit
	if buildkitTag() != "master" {
		t.Skip("policy eval provenance integration requires TEST_BUILDKIT_TAG=master")
	}
	if !isDockerContainerWorker(sb) {
		t.Skip("policy eval provenance integration requires docker-container worker")
	}
	// Base policy input support.
	skipNoCompatBuildKit(t, sb, ">= 0.26.0-0", "policy input requires BuildKit v0.26.0+")

	imageRef := createPolicyEvalProvenanceImage(t, sb)

	allowPolicyDir := tmpdir(
		t,
		fstest.CreateFile("policy.rego", []byte(`
package docker

default allow = false

allow if {
	input.image.hasProvenance
	input.image.provenance.predicateType == "https://slsa.dev/provenance/v1"
	input.image.provenance.frontend == "dockerfile.v0"
}

decision := {"allow": allow}
`), 0600),
	)

	allowCmd := buildxCmd(sb, withDir(allowPolicyDir), withArgs(
		"policy",
		"eval",
		"--filename",
		"policy",
		"docker-image://"+imageRef,
	))
	allowOut, err := allowCmd.CombinedOutput()
	if err != nil && strings.Contains(string(allowOut), "maximum attempts reached for resolving policy metadata") {
		t.Skip("policy eval provenance requires BuildKit ResolveAttestations support (currently master-only)")
	}
	require.NoError(t, err, string(allowOut))

	denyPolicyDir := tmpdir(
		t,
		fstest.CreateFile("policy.rego", []byte(`
package docker

default allow = false

allow if input.image.provenance.frontend == "not-a-real-frontend"

decision := {"allow": allow}
`), 0600),
	)

	denyCmd := buildxCmd(sb, withDir(denyPolicyDir), withArgs(
		"policy",
		"eval",
		"--filename",
		"policy",
		"docker-image://"+imageRef,
	))
	denyOut, err := denyCmd.CombinedOutput()
	if err != nil && strings.Contains(string(denyOut), "maximum attempts reached for resolving policy metadata") {
		t.Skip("policy eval provenance requires BuildKit ResolveAttestations support (currently master-only)")
	}
	require.Error(t, err, string(denyOut))
	require.Contains(t, string(denyOut), "policy denied")
}

func createPolicyEvalProvenanceImage(t *testing.T, sb integration.Sandbox) string {
	registry, err := sb.NewRegistry()
	if errors.Is(err, integration.ErrRequirements) {
		t.Skip(err.Error())
	}
	require.NoError(t, err)

	imageRef := registry + "/buildx/policy-eval-provenance:" + identity.NewID()
	dir := tmpdir(
		t,
		fstest.CreateFile("Dockerfile", []byte(`
FROM busybox:latest
RUN echo provenance > /proof
`), 0600),
	)

	buildCmd := buildxCmd(sb, withDir(dir), withArgs(
		"build",
		"--progress=plain",
		"--provenance=mode=max,version=v1",
		"--output=type=image,name="+imageRef+",push=true",
		dir,
	))
	buildOut, err := buildCmd.CombinedOutput()
	require.NoError(t, err, string(buildOut))

	return imageRef
}

func testPolicyEvalHTTP(t *testing.T, sb integration.Sandbox) {
	resp := &httpserver.Response{Content: []byte("policy-eval-http")}
	server := httpserver.NewTestServer(map[string]*httpserver.Response{
		"/file": resp,
	})
	defer server.Close()

	url := server.URL + "/file"
	queryURL := url + "?policy=allow"
	checksum := digest.FromBytes(resp.Content).String()
	parsedURL, err := urlpkg.Parse(server.URL)
	require.NoError(t, err)

	testCases := []struct {
		name            string
		policy          string
		sourceURL       string
		wantErrContains string
		needsChecksum   bool
	}{
		{
			name: "http-url-allow",
			policy: fmt.Sprintf(`
package docker

default allow = false

allow if input.http.url == "%s"

decision := {"allow": allow}
`, queryURL),
			sourceURL: queryURL,
		},
		{
			name: "http-host-allow",
			policy: fmt.Sprintf(`
package docker

default allow = false

allow if input.http.host == "%s"

decision := {"allow": allow}
`, parsedURL.Host),
			sourceURL: url,
		},
		{
			name: "http-path-allow",
			policy: `
package docker

default allow = false

allow if input.http.path == "/file"

decision := {"allow": allow}
`,
			sourceURL: url,
		},
		{
			name: "http-query-allow",
			policy: `
package docker

default allow = false

allow if input.http.query["policy"][_] == "allow"

decision := {"allow": allow}
`,
			sourceURL: queryURL,
		},
		{
			name: "http-checksum-allow",
			policy: fmt.Sprintf(`
package docker

default allow = false

allow if input.http.checksum == "%s"

decision := {"allow": allow}
`, checksum),
			sourceURL:     url,
			needsChecksum: true,
		},
		{
			name: "http-checksum-deny",
			policy: `
package docker

default allow = false

allow if input.http.checksum == "sha256:0000000000000000000000000000000000000000000000000000000000000000"

decision := {"allow": allow}
`,
			sourceURL:       url,
			wantErrContains: "policy denied",
			needsChecksum:   true,
		},
		{
			name: "http-host-deny",
			policy: `
package docker

default allow = false

allow if input.http.host == "example.invalid"

decision := {"allow": allow}
`,
			sourceURL:       url,
			wantErrContains: "policy denied",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.needsChecksum {
				sbDriver, _, _ := driverName(sb.Name())
				if sbDriver != "remote" {
					t.Skip("http checksum policy eval requires remote driver")
				}
				skipNoCompatBuildKit(t, sb, ">= 0.26.3-0", "http checksum policy input")
			}
			dir := tmpdir(
				t,
				fstest.CreateFile("policy.rego", []byte(tc.policy), 0600),
			)
			cmd := buildxCmd(sb, withDir(dir), withArgs(
				"policy",
				"eval",
				"--filename",
				"policy",
				tc.sourceURL,
			))
			out, err := cmd.CombinedOutput()
			if tc.wantErrContains == "" {
				require.NoError(t, err, string(out))
			} else {
				require.Error(t, err, string(out))
				require.Contains(t, string(out), tc.wantErrContains)
			}
		})
	}
}
