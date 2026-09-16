package execconn

import (
	"context"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"github.com/docker/buildx/driver/kubernetes/kubeclient"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

var errStreamEndedBeforeReady = errors.New("exec stream ended before the connection was established")

func ExecConn(ctx context.Context, restClient rest.Interface, restConfig *rest.Config, namespace, pod, container string, cmd []string) (net.Conn, error) {
	req := restClient.
		Post().
		Namespace(namespace).
		Resource("pods").
		Name(pod).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: container,
			Command:   cmd,
			Stdin:     true,
			Stdout:    true,
			Stderr:    true,
			TTY:       false,
		}, kubeclient.ParameterCodec())
	exec, err := remotecommand.NewSPDYExecutor(restConfig, "POST", req.URL())
	if err != nil {
		return nil, err
	}
	return newExecConn(ctx, exec)
}

// newExecConn wires a remotecommand.Executor's stdin/stdout streams up as a net.Conn.
// It is split from ExecConn to ease testing.
//
// StreamWithContext sets up the exec stream synchronously and then blocks for its
// whole lifetime, so it runs in a goroutine. newExecConn waits for the stream to be
// established before returning, otherwise a failed setup (e.g. "tls: internal error"
// on a not-yet-ready node) would only show up on the first Read/Write, after Dial has
// already reported success and past its retry. The executor reads stdin only once the
// stream is established, so the first stdin read is used as the readiness signal.
func newExecConn(ctx context.Context, exec remotecommand.Executor) (net.Conn, error) {
	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	kc := &kubeConn{
		stdin:      stdinW,
		stdout:     stdoutR,
		localAddr:  dummyAddr{network: "dummy", s: "dummy-0"},
		remoteAddr: dummyAddr{network: "dummy", s: "dummy-1"},
	}

	ready := make(chan struct{})
	stdin := &readyReader{r: stdinR, ready: ready}

	streamErr := make(chan error, 1)
	go func() {
		serr := exec.StreamWithContext(ctx, remotecommand.StreamOptions{
			Stdin:  stdin,
			Stdout: stdoutW,
			Stderr: os.Stderr,
			Tty:    false,
		})
		if serr != nil && serr != context.Canceled {
			logrus.Error(serr)
		}
		// Ensure the pipes are closed to unblock Read/Write on kubeConn and avoid infinite hangs.
		stdoutW.CloseWithError(serr)
		stdinR.CloseWithError(serr)
		streamErr <- serr
	}()

	select {
	case <-ready:
		return kc, nil
	case serr := <-streamErr:
		_ = kc.Close()
		if serr == nil {
			serr = errStreamEndedBeforeReady
		}
		return nil, serr
	case <-ctx.Done():
		_ = kc.Close()
		return nil, context.Cause(ctx)
	}
}

// readyReader closes ready on the first Read. The exec stream reads stdin only once its
// streams have been created, so the first read marks the connection as established.
type readyReader struct {
	r     io.Reader
	once  sync.Once
	ready chan struct{}
}

func (r *readyReader) Read(p []byte) (int, error) {
	r.once.Do(func() { close(r.ready) })
	return r.r.Read(p)
}

type kubeConn struct {
	stdin         io.WriteCloser
	stdout        io.ReadCloser
	stdioClosedMu sync.Mutex // for stdinClosed and stdoutClosed
	stdinClosed   bool
	stdoutClosed  bool
	localAddr     net.Addr
	remoteAddr    net.Addr
}

func (c *kubeConn) Write(p []byte) (int, error) {
	return c.stdin.Write(p)
}

func (c *kubeConn) Read(p []byte) (int, error) {
	return c.stdout.Read(p)
}

func (c *kubeConn) CloseWrite() error {
	err := c.stdin.Close()
	c.stdioClosedMu.Lock()
	c.stdinClosed = true
	c.stdioClosedMu.Unlock()
	return err
}
func (c *kubeConn) CloseRead() error {
	err := c.stdout.Close()
	c.stdioClosedMu.Lock()
	c.stdoutClosed = true
	c.stdioClosedMu.Unlock()
	return err
}

func (c *kubeConn) Close() error {
	var err error
	c.stdioClosedMu.Lock()
	stdinClosed := c.stdinClosed
	c.stdioClosedMu.Unlock()
	if !stdinClosed {
		err = c.CloseWrite()
	}
	c.stdioClosedMu.Lock()
	stdoutClosed := c.stdoutClosed
	c.stdioClosedMu.Unlock()
	if !stdoutClosed {
		err = c.CloseRead()
	}
	return err
}

func (c *kubeConn) LocalAddr() net.Addr {
	return c.localAddr
}
func (c *kubeConn) RemoteAddr() net.Addr {
	return c.remoteAddr
}
func (c *kubeConn) SetDeadline(t time.Time) error {
	return nil
}
func (c *kubeConn) SetReadDeadline(t time.Time) error {
	return nil
}
func (c *kubeConn) SetWriteDeadline(t time.Time) error {
	return nil
}

type dummyAddr struct {
	network string
	s       string
}

func (d dummyAddr) Network() string {
	return d.network
}

func (d dummyAddr) String() string {
	return d.s
}
