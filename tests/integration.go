package tests

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/containerd/continuity/fs/fstest"
	"github.com/moby/buildkit/util/testutil/integration"
	"github.com/stretchr/testify/require"
)

const defaultBuildKitTag = "buildx-stable-1"

var buildkitImage string

func tmpdir(t *testing.T, appliers ...fstest.Applier) string {
	t.Helper()
	tmpdir := t.TempDir()
	err := fstest.Apply(appliers...).Apply(tmpdir)
	require.NoError(t, err)
	return tmpdir
}

type cmdOpt func(*exec.Cmd)

func withEnv(env ...string) cmdOpt {
	return func(cmd *exec.Cmd) {
		cmd.Env = append(cmd.Env, env...)
	}
}

func withArgs(args ...string) cmdOpt {
	return func(cmd *exec.Cmd) {
		cmd.Args = append(cmd.Args, args...)
	}
}

func withDir(dir string) cmdOpt {
	return func(cmd *exec.Cmd) {
		cmd.Dir = dir
	}
}

func buildxCmd(sb integration.Sandbox, opts ...cmdOpt) *exec.Cmd {
	cmd := exec.Command("buildx")
	cmd.Env = append([]string{}, os.Environ()...)
	for _, opt := range opts {
		opt(cmd)
	}

	if builder := sb.Address(); builder != "" {
		cmd.Env = append(cmd.Env,
			"BUILDX_CONFIG="+buildxConfig(sb),
			"BUILDX_BUILDER="+builder,
		)
	}
	if context := sb.DockerAddress(); context != "" {
		cmd.Env = append(cmd.Env, "DOCKER_CONTEXT="+context)
	}
	if isExperimental() {
		cmd.Env = append(cmd.Env, "BUILDX_EXPERIMENTAL=1")
	}
	if v := os.Getenv("GO_TEST_COVERPROFILE"); v != "" {
		coverDir := filepath.Join(filepath.Dir(v), "helpers")
		cmd.Env = append(cmd.Env, "GOCOVERDIR="+coverDir)
	}

	return cmd
}

func dockerCmd(sb integration.Sandbox, opts ...cmdOpt) *exec.Cmd {
	cmd := exec.Command("docker")
	cmd.Env = append([]string{}, os.Environ()...)
	for _, opt := range opts {
		opt(cmd)
	}
	if context := sb.DockerAddress(); context != "" {
		cmd.Env = append(cmd.Env, "DOCKER_CONTEXT="+context)
	}
	return cmd
}

func buildxConfig(sb integration.Sandbox) string {
	if builder := sb.Address(); builder != "" {
		return "/tmp/buildx-" + builder
	}
	return ""
}

func isMobyWorker(sb integration.Sandbox) bool {
	name, _, hasFeature := driverName(sb.Name())
	return name == "docker" && !hasFeature
}

func isMobyContainerdSnapWorker(sb integration.Sandbox) bool {
	name, _, hasFeature := driverName(sb.Name())
	return name == "docker" && hasFeature
}

func isDockerWorker(sb integration.Sandbox) bool {
	name, _, _ := driverName(sb.Name())
	return name == "docker"
}

func isDockerContainerWorker(sb integration.Sandbox) bool {
	name, _, _ := driverName(sb.Name())
	return name == "docker-container"
}

func driverName(sbName string) (string, bool, bool) {
	name := sbName
	var hasVersion, hasFeature bool
	if b, _, ok := strings.Cut(sbName, "@"); ok {
		name = b
		hasVersion = true
	}
	if b, _, ok := strings.Cut(name, "+"); ok {
		name = b
		hasFeature = true
	}
	return name, hasVersion, hasFeature
}

func isExperimental() bool {
	if v, ok := os.LookupEnv("TEST_BUILDX_EXPERIMENTAL"); ok {
		vv, _ := strconv.ParseBool(v)
		return vv
	}
	return false
}

func buildkitTag() string {
	if v := os.Getenv("TEST_BUILDKIT_TAG"); v != "" {
		return v
	}
	return defaultBuildKitTag
}

var (
	bkvers   map[string]string
	bkversMu sync.Mutex
)

func buildkitVersion(t *testing.T, sb integration.Sandbox) string {
	bkversMu.Lock()
	defer bkversMu.Unlock()

	if bkvers == nil {
		bkvers = make(map[string]string)
	}

	ver, ok := bkvers[sb.Name()]
	if !ok {
		out, err := inspectCmd(sb, withArgs(sb.Address()))
		require.NoError(t, err, out)
		for _, line := range strings.Split(out, "\n") {
			if v, ok := strings.CutPrefix(line, "BuildKit version:"); ok {
				ver = strings.TrimSpace(v)
				bkvers[sb.Name()] = ver
			}
		}
		if ver == "" {
			t.Logf("BuildKit version not found in inspect output, extract it from the image.\n%s", out)
			undockBin, err := exec.LookPath("undock")
			require.NoError(t, err, "undock not found")

			destDir := t.TempDir()
			t.Cleanup(func() {
				os.RemoveAll(destDir)
			})

			cmd := exec.Command(undockBin, "--cachedir", "/root/.cache/undock", "--include", "/usr/bin/buildkitd", "--rm-dist", buildkitImage, destDir)
			require.NoErrorf(t, cmd.Run(), "failed to extract buildkitd binary from %q", buildkitImage)

			cmd = exec.Command(filepath.Join(destDir, "usr", "bin", "buildkitd"), "--version")
			out, err := cmd.CombinedOutput()
			require.NoErrorf(t, err, "failed to get BuildKit version from %q: %s", buildkitImage, string(out))

			v := strings.Fields(strings.TrimSpace(string(out)))
			if len(v) != 4 {
				require.Fail(t, "unexpected version format: "+strings.TrimSpace(string(out)))
			}
			ver = v[2]
			bkvers[sb.Name()] = ver
		}
	}

	return ver
}

func matchesBuildKitVersion(t *testing.T, sb integration.Sandbox, constraint string) bool {
	c, err := semver.NewConstraint(constraint)
	if err != nil {
		return false
	}
	v, err := semver.NewVersion(buildkitVersion(t, sb))
	if err != nil {
		// if the version is not a valid semver, we assume it matches (master)
		return true
	}
	return c.Check(v)
}

func skipNoCompatBuildKit(t *testing.T, sb integration.Sandbox, constraint string, msg string) {
	if !matchesBuildKitVersion(t, sb, constraint) {
		t.Skipf("buildkit version %s does not match %s constraint (%s)", buildkitVersion(t, sb), constraint, msg)
	}
}

func ptrstr(s interface{}) *string {
	var n *string
	if reflect.ValueOf(s).Kind() == reflect.String {
		ss := s.(string)
		n = &ss
	}
	return n
}
