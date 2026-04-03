package helpers

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/moby/buildkit/util/testutil/integration"
	"github.com/pkg/errors"
)

const (
	k3sBin        = "k3s"
	kubectlBin    = "kubectl"
	k3sNodeName   = "integrationk3s"
	k3sWaitWindow = 3 * time.Minute
	k3sWaitDelay  = 5 * time.Second
)

func NewK3sServer(cfg *integration.BackendConfig) (kubeConfig string, cl func() error, err error) {
	if _, err := exec.LookPath(k3sBin); err != nil {
		return "", nil, errors.Wrapf(err, "failed to lookup %s binary", k3sBin)
	}
	if _, err := exec.LookPath(kubectlBin); err != nil {
		return "", nil, errors.Wrapf(err, "failed to lookup %s binary", kubectlBin)
	}

	deferF := &integration.MultiCloser{}
	cl = deferF.F()
	defer func() {
		if err != nil {
			deferF.F()()
			cl = nil
		}
	}()

	cfgfile, err := os.CreateTemp("", "kubeconfig*.yml")
	if err != nil {
		return "", nil, err
	}
	if err := cfgfile.Close(); err != nil {
		return "", nil, err
	}
	kubeConfig = cfgfile.Name()
	deferF.Append(func() error {
		return os.Remove(cfgfile.Name())
	})

	k3sDataDir, err := os.MkdirTemp("", "kubedata")
	if err != nil {
		return "", nil, err
	}
	deferF.Append(func() error {
		return os.RemoveAll(k3sDataDir)
	})

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	_ = l.Close()
	lport := strconv.Itoa(l.Addr().(*net.TCPAddr).Port)

	k3sGlobalDir := "/etc/rancher/k3s"
	if err := os.MkdirAll(k3sGlobalDir, 0o755); err != nil {
		return "", nil, err
	}

	registriesFile := filepath.Join(k3sGlobalDir, "registries.yaml")
	if cfg.Mirror != "" {
		mirror := cfg.Mirror
		if port, ok := strings.CutPrefix(mirror, "localhost:"); ok {
			mirror = "host.k3s.internal:" + port
		}
		dt := []byte(fmt.Sprintf(`
mirrors:
  "docker.io":
    endpoint:
      - "%s"

configs:
  "%s":
    insecure_skip_verify: true
`, mirror, mirror))
		if err := os.WriteFile(registriesFile, dt, 0o644); err != nil {
			return "", nil, err
		}
		deferF.Append(func() error {
			if err := os.Remove(registriesFile); errors.Is(err, os.ErrNotExist) {
				return nil
			} else {
				return err
			}
		})
	}

	cmd := exec.Command(
		k3sBin,
		"server",
		"--bind-address", "0.0.0.0",
		"--https-listen-port", lport,
		"--data-dir", k3sDataDir,
		"--write-kubeconfig", cfgfile.Name(),
		"--write-kubeconfig-mode", "666",
		"--node-name", k3sNodeName,
	)
	stop, err := integration.StartCmd(cmd, cfg.Logs)
	if err != nil {
		return "", nil, err
	}

	if err = waitK3s(cfg, kubeConfig); err != nil {
		stop()
		containerdLogs, _ := os.ReadFile(filepath.Join(k3sDataDir, "agent", "containerd", "containerd.log"))
		containerdConfig, _ := os.ReadFile(filepath.Join(k3sDataDir, "agent", "etc", "containerd", "config.toml"))
		registries, _ := os.ReadFile(registriesFile)
		return "", nil, errors.Wrapf(err, "k3s did not start up: %s\ncontainerd.log: %s\ncontainerd.config.toml: %s\nregistries.yaml: %s", formatLogs(cfg.Logs), containerdLogs, containerdConfig, registries)
	}

	deferF.Append(stop)
	return kubeConfig, cl, nil
}

func waitK3s(cfg *integration.BackendConfig, kubeConfig string) error {
	logbuf := new(bytes.Buffer)
	defer func() {
		if logbuf.Len() > 0 {
			cfg.Logs["waitK3s: "] = logbuf
		}
	}()

	deadline := time.Now().Add(k3sWaitWindow)
	var lastErr error
	for time.Now().Before(deadline) {
		cmd := exec.Command(kubectlBin, "--kubeconfig", kubeConfig, "wait", "--timeout=5s", "--for=condition=Ready", "node/"+k3sNodeName)
		out, err := cmd.CombinedOutput()
		if err == nil && bytes.Contains(out, []byte("condition met")) {
			return nil
		}
		lastErr = errors.Wrapf(err, "node is not ready: %s %s", cmd.String(), string(out))
		logbuf.Reset()
		logbuf.WriteString(lastErr.Error())
		time.Sleep(k3sWaitDelay)
	}
	if lastErr == nil {
		lastErr = errors.New("node did not become ready")
	}
	return lastErr
}

func formatLogs(m map[string]*bytes.Buffer) string {
	var ss []string
	for k, b := range m {
		if b != nil {
			ss = append(ss, fmt.Sprintf("%q:%s", k, b.String()))
		}
	}
	return strings.Join(ss, ",")
}
