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
	"github.com/docker/buildx/build"
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

type MonitorBuildResult struct {
	Resp *client.SolveResponse
	Err  error
}

// RunMonitor provides an interactive session for running and managing containers via specified IO.
func RunMonitor(ctx context.Context, curRef string, options *controllerapi.BuildOptions, invokeConfig *controllerapi.InvokeConfig, c control.BuildxController, stdin io.ReadCloser, stdout io.WriteCloser, stderr console.File, progress *progress.Printer) (*MonitorBuildResult, error) {
	defer func() {
		if err := c.Disconnect(ctx, curRef); err != nil {
			logrus.Warnf("disconnect error: %v", err)
		}
	}()

	if err := progress.Pause(); err != nil {
		return nil, err
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
		BuildxController: c,
		invokeIO:         invokeForwarder,
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
	m.ref.Store(curRef)

	// Start container automatically
	fmt.Fprintf(stdout, "Launching interactive container. Press Ctrl-a-c to switch to monitor console\n")
	invokeConfig.Rollback = false
	invokeConfig.Initial = false
	id := m.Rollback(ctx, invokeConfig)
	fmt.Fprintf(stdout, "Interactive container was restarted with process %q. Press Ctrl-a-c to switch to the new container\n", id)

	availableCommands := []types.Command{
		commands.NewReloadCmd(m, stdout, progress, options, invokeConfig),
		commands.NewRollbackCmd(m, invokeConfig, stdout),
		commands.NewListCmd(m, stdout),
		commands.NewDisconnectCmd(m),
		commands.NewKillCmd(m),
		commands.NewAttachCmd(m, stdout),
		commands.NewExecCmd(m, invokeConfig, stdout),
		commands.NewPsCmd(m, stdout),
	}
	registeredCommands := make(map[string]types.Command)
	for _, c := range availableCommands {
		registeredCommands[c.Info().Name] = c
	}
	additionalHelpMessages := map[string]string{
		"help": "shows this message. Optionally pass a command name as an argument to print the detailed usage.",
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
					if len(args) >= 2 {
						printHelpMessageOfCommand(stdout, args[1], registeredCommands, additionalHelpMessages)
						continue
					}
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
			return m.lastBuildResult, nil
		case err := <-errCh:
			m.close()
			return m.lastBuildResult, err
		case <-monitorDisableCh:
		}
		monitorForwarder.SetOut(nil)
	}
}

func printHelpMessageOfCommand(out io.Writer, name string, registeredCommands map[string]types.Command, additional map[string]string) {
	var target types.Command
	if c, ok := registeredCommands[name]; ok {
		target = c
	} else {
		fmt.Fprintf(out, "monitor: no help message for %q\n", name)
		printHelpMessage(out, registeredCommands, additional)
		return
	}
	fmt.Fprintln(out, target.Info().HelpMessage)
	if h := target.Info().HelpMessageLong; h != "" {
		fmt.Fprintln(out, h)
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
	control.BuildxController
	ref atomic.Value

	muxIO        *ioset.MuxIO
	invokeIO     *ioset.Forwarder
	invokeCancel func()
	attachedPid  atomic.Value

	lastBuildResult *MonitorBuildResult
}

func (m *monitor) Build(ctx context.Context, options *controllerapi.BuildOptions, in io.ReadCloser, progress progress.Writer) (ref string, resp *client.SolveResponse, input *build.Inputs, err error) {
	ref, resp, _, err = m.BuildxController.Build(ctx, options, in, progress)
	m.lastBuildResult = &MonitorBuildResult{Resp: resp, Err: err} // Record build result
	return
}

func (m *monitor) DisconnectSession(ctx context.Context, targetID string) error {
	return m.Disconnect(ctx, targetID)
}

func (m *monitor) AttachSession(ref string) {
	m.ref.Store(ref)
}

func (m *monitor) AttachedSessionID() string {
	return m.ref.Load().(string)
}

func (m *monitor) Rollback(ctx context.Context, cfg *controllerapi.InvokeConfig) string {
	pid := identity.NewID()
	cfg1 := cfg
	cfg1.Rollback = true
	return m.startInvoke(ctx, pid, cfg1)
}

func (m *monitor) Exec(ctx context.Context, cfg *controllerapi.InvokeConfig) string {
	return m.startInvoke(ctx, identity.NewID(), cfg)
}

func (m *monitor) Attach(ctx context.Context, pid string) {
	m.startInvoke(ctx, pid, &controllerapi.InvokeConfig{})
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

func (m *monitor) startInvoke(ctx context.Context, pid string, cfg *controllerapi.InvokeConfig) string {
	if m.invokeCancel != nil {
		m.invokeCancel() // Finish existing attach
	}
	if len(cfg.Entrypoint) == 0 && len(cfg.Cmd) == 0 {
		cfg.Entrypoint = []string{"sh"} // launch shell by default
		cfg.Cmd = []string{}
		cfg.NoCmd = false
	}
	go func() {
		// Start a new invoke
		if err := m.invoke(ctx, pid, cfg); err != nil {
			if errors.Is(err, context.Canceled) {
				logrus.Debugf("process canceled: %v", err)
			} else {
				logrus.Errorf("invoke: %v", err)
			}
		}
		if pid == m.attachedPid.Load() {
			m.attachedPid.Store("")
		}
	}()
	m.attachedPid.Store(pid)
	return pid
}

func (m *monitor) invoke(ctx context.Context, pid string, cfg *controllerapi.InvokeConfig) error {
	m.muxIO.Enable(1)
	defer m.muxIO.Disable(1)
	if err := m.muxIO.SwitchTo(1); err != nil {
		return errors.Errorf("failed to switch to process IO: %v", err)
	}
	if m.AttachedSessionID() == "" {
		return nil
	}
	invokeCtx, invokeCancel := context.WithCancelCause(ctx)

	containerIn, containerOut := ioset.Pipe()
	m.invokeIO.SetOut(&containerOut)
	waitInvokeDoneCh := make(chan struct{})
	var cancelOnce sync.Once
	invokeCancelAndDetachFn := func() {
		cancelOnce.Do(func() {
			containerIn.Close()
			m.invokeIO.SetOut(nil)
			invokeCancel(errors.WithStack(context.Canceled))
		})
		<-waitInvokeDoneCh
	}
	defer invokeCancelAndDetachFn()
	m.invokeCancel = invokeCancelAndDetachFn

	err := m.Invoke(invokeCtx, m.AttachedSessionID(), pid, cfg, containerIn.Stdin, containerIn.Stdout, containerIn.Stderr)
	close(waitInvokeDoneCh)

	return err
}

type nopCloser struct {
	io.Writer
}

func (c nopCloser) Close() error { return nil }
