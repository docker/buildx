package monitor

import (
	"context"
	"fmt"
	"io"
	"sort"
	"sync"
	"sync/atomic"
	"text/tabwriter"

	"github.com/containerd/console"
	"github.com/docker/buildx/controller/control"
	controllerapi "github.com/docker/buildx/controller/pb"
	"github.com/docker/buildx/monitor/commands"
	"github.com/docker/buildx/monitor/types"
	"github.com/docker/buildx/util/ioset"
	"github.com/docker/buildx/util/progress"
	"github.com/google/shlex"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/identity"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"golang.org/x/term"
)

// RunMonitor provides an interactive session for running and managing containers via specified IO.
func RunMonitor(ctx context.Context, curRef string, options *controllerapi.BuildOptions, invokeConfig controllerapi.InvokeConfig, c control.BuildxController, stdin io.ReadCloser, stdout io.WriteCloser, stderr console.File, progress *progress.Printer) error {
	defer func() {
		if err := c.Disconnect(ctx, curRef); err != nil {
			logrus.Warnf("disconnect error: %v", err)
		}
	}()

	if err := progress.Pause(); err != nil {
		return err
	}
	defer progress.Unpause()

	monitorIn, monitorOut := ioset.Pipe()
	defer func() {
		monitorIn.Close()
	}()
	monitorEnableCh := make(chan struct{})
	monitorDisableCh := make(chan struct{})
	monitorOutCtx := ioset.MuxOut{
		Out:         monitorOut,
		EnableHook:  func() { monitorEnableCh <- struct{}{} },
		DisableHook: func() { monitorDisableCh <- struct{}{} },
	}

	containerIn, containerOut := ioset.Pipe()
	defer func() {
		containerIn.Close()
	}()
	containerOutCtx := ioset.MuxOut{
		Out: containerOut,
		// send newline to hopefully get the prompt; TODO: better UI (e.g. reprinting the last line)
		EnableHook:  func() { containerOut.Stdin.Write([]byte("\n")) },
		DisableHook: func() {},
	}

	invokeForwarder := ioset.NewForwarder()
	invokeForwarder.SetIn(&containerIn)
	m := &monitor{
		controllerRef: newControllerRef(c, curRef),
		invokeIO:      invokeForwarder,
		muxIO: ioset.NewMuxIO(ioset.In{
			Stdin:  io.NopCloser(stdin),
			Stdout: nopCloser{stdout},
			Stderr: nopCloser{stderr},
		}, []ioset.MuxOut{monitorOutCtx, containerOutCtx}, 1, func(prev int, res int) string {
			if prev == 0 && res == 0 {
				// No toggle happened because container I/O isn't enabled.
				return "Process isn't attached (previous \"exec\" exited?). Use \"attach\" for attaching or \"rollback\" or \"exec\" for running new one.\n"
			}
			return "Switched IO\n"
		}),
	}

	// Start container automatically
	fmt.Fprintf(stdout, "Launching interactive container. Press Ctrl-a-c to switch to monitor console\n")
	invokeConfig.Rollback = false
	invokeConfig.Initial = false
	id := m.Rollback(ctx, invokeConfig)
	fmt.Fprintf(stdout, "Interactive container was restarted with process %q. Press Ctrl-a-c to switch to the new container\n", id)

	registeredCommands := map[string]types.Command{
		"reload":     commands.NewReloadCmd(m, stdout, progress, options, invokeConfig),
		"rollback":   commands.NewRollbackCmd(m, invokeConfig, stdout),
		"list":       commands.NewListCmd(m, stdout),
		"disconnect": commands.NewDisconnectCmd(m),
		"kill":       commands.NewKillCmd(m),
		"attach":     commands.NewAttachCmd(m, stdout),
		"exec":       commands.NewExecCmd(m, invokeConfig, stdout),
		"ps":         commands.NewPsCmd(m, stdout),
	}
	additionalHelpMessages := map[string]string{
		"help": "shows this message",
		"exit": "exits monitor",
	}

	// Serve monitor commands
	monitorForwarder := ioset.NewForwarder()
	monitorForwarder.SetIn(&monitorIn)
	for {
		<-monitorEnableCh
		in, out := ioset.Pipe()
		monitorForwarder.SetOut(&out)
		doneCh, errCh := make(chan struct{}), make(chan error)
		go func() {
			defer close(doneCh)
			defer in.Close()
			go func() {
				<-ctx.Done()
				in.Close()
			}()
			t := term.NewTerminal(readWriter{in.Stdin, in.Stdout}, "(buildx) ")
			for {
				l, err := t.ReadLine()
				if err != nil {
					if err != io.EOF {
						errCh <- err
						return
					}
					return
				}
				args, err := shlex.Split(l)
				if err != nil {
					fmt.Fprintf(stdout, "monitor: failed to parse command: %v", err)
					continue
				} else if len(args) == 0 {
					continue
				}

				// Builtin commands
				switch args[0] {
				case "":
					// nop
					continue
				case "exit":
					return
				case "help":
					printHelpMessage(stdout, registeredCommands, additionalHelpMessages)
					continue
				default:
				}

				// Registered commands
				cmdname := args[0]
				if cm, ok := registeredCommands[cmdname]; ok {
					if err := cm.Exec(ctx, args); err != nil {
						fmt.Fprintf(stdout, "%s: %v\n", cmdname, err)
					}
				} else {
					fmt.Fprintf(stdout, "monitor: unknown command: %q\n", l)
					printHelpMessage(stdout, registeredCommands, additionalHelpMessages)
				}
			}
		}()
		select {
		case <-doneCh:
			m.close()
			return nil
		case err := <-errCh:
			m.close()
			return err
		case <-monitorDisableCh:
		}
		monitorForwarder.SetOut(nil)
	}
}

func printHelpMessage(out io.Writer, registeredCommands map[string]types.Command, additional map[string]string) {
	var names []string
	for name := range registeredCommands {
		names = append(names, name)
	}
	for name := range additional {
		names = append(names, name)
	}
	sort.Strings(names)
	fmt.Fprint(out, "Available commands are:\n")
	w := new(tabwriter.Writer)
	w.Init(out, 0, 8, 0, '\t', 0)
	for _, name := range names {
		var mes string
		if c, ok := registeredCommands[name]; ok {
			mes = c.Info().HelpMessage
		} else if m, ok := additional[name]; ok {
			mes = m
		} else {
			continue
		}
		fmt.Fprintln(w, "  "+name+"\t"+mes)
	}
	w.Flush()
}

type readWriter struct {
	io.Reader
	io.Writer
}

type monitor struct {
	*controllerRef

	muxIO        *ioset.MuxIO
	invokeIO     *ioset.Forwarder
	invokeCancel func()
	attachedPid  atomic.Value
}

func (m *monitor) DisconnectSession(ctx context.Context, targetID string) error {
	return m.controllerRef.raw().Disconnect(ctx, targetID)
}

func (m *monitor) AttachSession(ref string) {
	m.controllerRef.ref.Store(ref)
}

func (m *monitor) AttachedSessionID() string {
	return m.controllerRef.ref.Load().(string)
}

func (m *monitor) Rollback(ctx context.Context, cfg controllerapi.InvokeConfig) string {
	pid := identity.NewID()
	cfg1 := cfg
	cfg1.Rollback = true
	return m.startInvoke(ctx, pid, cfg1)
}

func (m *monitor) Exec(ctx context.Context, cfg controllerapi.InvokeConfig) string {
	return m.startInvoke(ctx, identity.NewID(), cfg)
}

func (m *monitor) Attach(ctx context.Context, pid string) {
	m.startInvoke(ctx, pid, controllerapi.InvokeConfig{})
}

func (m *monitor) Detach() {
	if m.invokeCancel != nil {
		m.invokeCancel() // Finish existing attach
	}
}

func (m *monitor) AttachedPID() string {
	return m.attachedPid.Load().(string)
}

func (m *monitor) close() {
	m.Detach()
}

func (m *monitor) startInvoke(ctx context.Context, pid string, cfg controllerapi.InvokeConfig) string {
	if m.invokeCancel != nil {
		m.invokeCancel() // Finish existing attach
	}
	if len(cfg.Entrypoint) == 0 && len(cfg.Cmd) == 0 {
		cfg.Entrypoint = []string{"sh"} // launch shell by default
	}
	go func() {
		// Start a new invoke
		if err := m.invoke(ctx, pid, cfg); err != nil {
			logrus.Debugf("invoke error: %v", err)
		}
		if pid == m.attachedPid.Load() {
			m.attachedPid.Store("")
		}
	}()
	m.attachedPid.Store(pid)
	return pid
}

func (m *monitor) invoke(ctx context.Context, pid string, cfg controllerapi.InvokeConfig) error {
	m.muxIO.Enable(1)
	defer m.muxIO.Disable(1)
	if err := m.muxIO.SwitchTo(1); err != nil {
		return errors.Errorf("failed to switch to process IO: %v", err)
	}
	if m.AttachedSessionID() == "" {
		return nil
	}
	invokeCtx, invokeCancel := context.WithCancel(ctx)

	containerIn, containerOut := ioset.Pipe()
	m.invokeIO.SetOut(&containerOut)
	waitInvokeDoneCh := make(chan struct{})
	var cancelOnce sync.Once
	invokeCancelAndDetachFn := func() {
		cancelOnce.Do(func() {
			containerIn.Close()
			m.invokeIO.SetOut(nil)
			invokeCancel()
		})
		<-waitInvokeDoneCh
	}
	defer invokeCancelAndDetachFn()
	m.invokeCancel = invokeCancelAndDetachFn

	err := m.controllerRef.invoke(invokeCtx, pid, cfg, containerIn.Stdin, containerIn.Stdout, containerIn.Stderr)
	close(waitInvokeDoneCh)

	return err
}

func newControllerRef(c control.BuildxController, ref string) *controllerRef {
	cr := &controllerRef{c: c}
	cr.ref.Store(ref)
	return cr
}

type controllerRef struct {
	c   control.BuildxController
	ref atomic.Value
}

func (c *controllerRef) raw() control.BuildxController {
	return c.c
}

func (c *controllerRef) getRef() (string, error) {
	ref := c.ref.Load()
	if ref == "" {
		return "", errors.Errorf("client is not attached to a session")
	}
	return ref.(string), nil
}

func (c *controllerRef) Build(ctx context.Context, options controllerapi.BuildOptions, in io.ReadCloser, progress progress.Writer) (ref string, resp *client.SolveResponse, err error) {
	return c.c.Build(ctx, options, in, progress)
}

func (c *controllerRef) invoke(ctx context.Context, pid string, options controllerapi.InvokeConfig, ioIn io.ReadCloser, ioOut io.WriteCloser, ioErr io.WriteCloser) error {
	ref, err := c.getRef()
	if err != nil {
		return err
	}
	return c.c.Invoke(ctx, ref, pid, options, ioIn, ioOut, ioErr)
}

func (c *controllerRef) Kill(ctx context.Context) error {
	return c.c.Kill(ctx)
}

func (c *controllerRef) List(ctx context.Context) (refs []string, _ error) {
	return c.c.List(ctx)
}

func (c *controllerRef) ListProcesses(ctx context.Context) (infos []*controllerapi.ProcessInfo, retErr error) {
	ref, err := c.getRef()
	if err != nil {
		return nil, err
	}
	return c.c.ListProcesses(ctx, ref)
}

func (c *controllerRef) DisconnectProcess(ctx context.Context, pid string) error {
	ref, err := c.getRef()
	if err != nil {
		return err
	}
	return c.c.DisconnectProcess(ctx, ref, pid)
}

func (c *controllerRef) Inspect(ctx context.Context) (*controllerapi.InspectResponse, error) {
	ref, err := c.getRef()
	if err != nil {
		return nil, err
	}
	return c.c.Inspect(ctx, ref)
}

func (c *controllerRef) Disconnect(ctx context.Context) error {
	ref, err := c.getRef()
	if err != nil {
		return err
	}
	return c.c.Disconnect(ctx, ref)
}

type nopCloser struct {
	io.Writer
}

func (c nopCloser) Close() error { return nil }
