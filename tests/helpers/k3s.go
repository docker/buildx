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

	"github.com/cenkalti/backoff/v4"
	"github.com/moby/buildkit/util/testutil/integration"
	"github.com/pkg/errors"
)

const (
	k3sBin     = "k3s"
	kubeCtlBin = "kubectl"
)

func NewK3sServer(cfg *integration.BackendConfig) (kubeConfig string, cl func() error, err error) {
	if _, err := exec.LookPath(k3sBin); err != nil {
		return "", nil, errors.Wrapf(err, "failed to lookup %s binary", k3sBin)
	}
	if _, err := exec.LookPath(kubeCtlBin); err != nil {
		return "", nil, errors.Wrapf(err, "failed to lookup %s binary", kubeCtlBin)
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

	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return "", nil, err
	}
	_ = l.Close()

	lport := strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
	nodeName := "integrationk3s"

	stop, err := integration.StartCmd(exec.Command(k3sBin, "server",
		"--bind-address", "127.0.0.1",
		"--https-listen-port", lport,
		"--data-dir", k3sDataDir, // write to /tmp for overlayfs support
		"--write-kubeconfig", cfgfile.Name(),
		"--write-kubeconfig-mode", "666",
		"--node-name", nodeName,
	), cfg.Logs)
	if err != nil {
		return "", nil, err
	}

	if err = waitK3s(cfg, kubeConfig, nodeName); err != nil {
		stop()
		containerdLogs, _ := os.ReadFile(filepath.Join(k3sDataDir, "agent", "containerd", "containerd.log"))
		if len(containerdLogs) > 0 {
			return "", nil, errors.Wrapf(err, "k3s did not start up: %s\ncontainerd.log: %s", formatLogs(cfg.Logs), containerdLogs)
		}
		return "", nil, errors.Wrapf(err, "k3s did not start up: %s", formatLogs(cfg.Logs))
	}

	deferF.Append(stop)
	return
}

func waitK3s(cfg *integration.BackendConfig, kubeConfig string, nodeName string) error {
	logbuf := new(bytes.Buffer)
	defer func() {
		if logbuf.Len() > 0 {
			cfg.Logs["waitK3s: "] = logbuf
		}
	}()

	boff := backoff.NewExponentialBackOff()
	boff.InitialInterval = 5 * time.Second
	boff.MaxInterval = 10 * time.Second
	boff.MaxElapsedTime = 3 * time.Minute

	if err := backoff.Retry(func() error {
		cmd := exec.Command(kubeCtlBin, "--kubeconfig", kubeConfig, "wait", "--for=condition=Ready", "node/"+nodeName)
		out, err := cmd.CombinedOutput()
		if err == nil && bytes.Contains(out, []byte("condition met")) {
			return nil
		}
		return errors.Wrapf(err, "node is not ready: %s %s", cmd.String(), string(out))
	}, boff); err != nil {
		logbuf.WriteString(errors.Unwrap(err).Error())
		return err
	}

	return nil
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
