package monitor

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"text/tabwriter"

	"github.com/containerd/console"
	"github.com/docker/buildx/build"
	"github.com/docker/buildx/monitor/commands"
	"github.com/docker/buildx/monitor/processes"
	"github.com/docker/buildx/monitor/types"
	"github.com/docker/buildx/util/ioset"
	"github.com/docker/buildx/util/progress"
	"github.com/google/shlex"
	"github.com/moby/buildkit/identity"
	"github.com/moby/buildkit/solver/errdefs"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"golang.org/x/term"
)

var ErrReload = errors.New("monitor: reload")

type Monitor struct {
	invokeConfig *build.InvokeConfig
	printer      *progress.Printer

	stdin  *ioset.SingleForwarder
	stdout io.WriteCloser
	stderr io.WriteCloser

	res *build.ResultHandle
	idx int
	mu  sync.Mutex
}

func New(cfg *build.InvokeConfig, stdin io.ReadCloser, stdout, stderr io.WriteCloser, printer *progress.Printer) *Monitor {
	m := &Monitor{
		invokeConfig: cfg,
		printer:      printer,
		stdin:        ioset.NewSingleForwarder(),
		stdout:       stdout,
		stderr:       stderr,
	}
	m.stdin.SetReader(stdin)
	return m
}

func (m *Monitor) Handler() build.Handler {
	return build.Handler{
		OnResult: func(driverIndex int, gotRes *build.ResultHandle) {
			m.mu.Lock()
			defer m.mu.Unlock()

			if m.res == nil || driverIndex < m.idx {
				m.idx, m.res = driverIndex, gotRes
			}
		},
	}
}

func (m *Monitor) Run(ctx context.Context, buildErr error) error {
	defer m.reset()

	if !m.invokeConfig.NeedsDebug(buildErr) {
		return nil
	}

	// Print errors before launching monitor
	if err := printError(buildErr, m.printer); err != nil {
		logrus.Warnf("failed to print error information: %v", err)
	}

	pr, pw := io.Pipe()
	m.stdin.SetWriter(pw, func() io.WriteCloser {
		pw.Close() // propagate EOF
		return nil
	})

	con := console.Current()
	if err := con.SetRaw(); err != nil {
		return errors.Errorf("failed to configure terminal: %v", err)
	}
	defer con.Reset()

	monitorErr := RunMonitor(ctx, m.invokeConfig, m.res, pr, m.stdout, m.stderr, m.printer)
	if err := pw.Close(); err != nil {
		logrus.Debug("failed to close monitor stdin pipe reader")
	}
	return monitorErr
}

func (m *Monitor) reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.idx = 0
	if m.res != nil {
		m.res.Done()
		m.res = nil
	}
}

func (m *Monitor) Close() error {
	return m.stdin.Close()
}

// RunMonitor provides an interactive session for running and managing containers via specified IO.
func RunMonitor(ctx context.Context, invokeConfig *build.InvokeConfig, rCtx *build.ResultHandle, stdin io.ReadCloser, stdout, stderr io.WriteCloser, progress *progress.Printer) error {
	progress.Pause()
	defer progress.Resume()

	defer stdin.Close()

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
		rCtx:      rCtx,
		processes: processes.NewManager(),
		invokeIO:  invokeForwarder,
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
	m.ctx, m.cancel = context.WithCancelCause(context.Background())

	defer func() {
		if err := m.Close(); err != nil {
			logrus.Warnf("close error: %v", err)
		}
	}()

	// Start container automatically
	fmt.Fprintf(stdout, "Launching interactive container. Press Ctrl-a-c to switch to monitor console\n")
	invokeConfig.Rollback = false
	invokeConfig.Initial = false
	id := m.Rollback(ctx, invokeConfig)
	fmt.Fprintf(stdout, "Interactive container was restarted with process %q. Press Ctrl-a-c to switch to the new container\n", id)

	availableCommands := []types.Command{
		commands.NewReloadCmd(m),
		commands.NewRollbackCmd(m, invokeConfig, stdout),
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
				if err := m.ctx.Err(); err != nil {
					errCh <- context.Cause(m.ctx)
					return
				}

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
			return nil
		case err := <-errCh:
			m.close()
			return err
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
	ctx    context.Context
	cancel context.CancelCauseFunc

	rCtx *build.ResultHandle

	muxIO        *ioset.MuxIO
	invokeIO     *ioset.Forwarder
	invokeCancel func()
	attachedPid  atomic.Value

	processes *processes.Manager
}

func (m *monitor) Invoke(ctx context.Context, pid string, cfg *build.InvokeConfig, ioIn io.ReadCloser, ioOut io.WriteCloser, ioErr io.WriteCloser) error {
	proc, ok := m.processes.Get(pid)
	if !ok {
		// Start a new process.
		if m.rCtx == nil {
			return errors.New("no build result is registered")
		}
		var err error
		proc, err = m.processes.StartProcess(pid, m.rCtx, cfg)
		if err != nil {
			return err
		}
	}

	// Attach containerIn to this process
	ioCancelledCh := make(chan struct{})
	proc.ForwardIO(&ioset.In{Stdin: ioIn, Stdout: ioOut, Stderr: ioErr}, func(error) { close(ioCancelledCh) })

	select {
	case <-ioCancelledCh:
		return errors.Errorf("io cancelled")
	case err := <-proc.Done():
		return err
	case <-ctx.Done():
		return context.Cause(ctx)
	}
}

func (m *monitor) Rollback(ctx context.Context, cfg *build.InvokeConfig) string {
	pid := identity.NewID()
	cfg1 := cfg
	cfg1.Rollback = true
	return m.startInvoke(ctx, pid, cfg1)
}

func (m *monitor) Exec(ctx context.Context, cfg *build.InvokeConfig) string {
	return m.startInvoke(ctx, identity.NewID(), cfg)
}

func (m *monitor) Attach(ctx context.Context, pid string) {
	m.startInvoke(ctx, pid, &build.InvokeConfig{})
}

func (m *monitor) Detach() {
	if m.invokeCancel != nil {
		m.invokeCancel() // Finish existing attach
	}
}

func (m *monitor) Reload() {
	m.cancel(ErrReload)
}

func (m *monitor) AttachedPID() string {
	return m.attachedPid.Load().(string)
}

func (m *monitor) close() {
	m.Detach()
}

func (m *monitor) startInvoke(ctx context.Context, pid string, cfg *build.InvokeConfig) string {
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

func (m *monitor) invoke(ctx context.Context, pid string, cfg *build.InvokeConfig) error {
	m.muxIO.Enable(1)
	defer m.muxIO.Disable(1)
	if err := m.muxIO.SwitchTo(1); err != nil {
		return errors.Errorf("failed to switch to process IO: %v", err)
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

	err := m.Invoke(invokeCtx, pid, cfg, containerIn.Stdin, containerIn.Stdout, containerIn.Stderr)
	close(waitInvokeDoneCh)

	return err
}

func (m *monitor) Close() error {
	m.cancelRunningProcesses()
	return nil
}

func (m *monitor) ListProcesses(ctx context.Context) (infos []*processes.ProcessInfo, retErr error) {
	return m.processes.ListProcesses(), nil
}

func (m *monitor) DisconnectProcess(ctx context.Context, pid string) error {
	return m.processes.DeleteProcess(pid)
}

func (m *monitor) cancelRunningProcesses() {
	m.processes.CancelRunningProcesses()
}

type nopCloser struct {
	io.Writer
}

func (c nopCloser) Close() error { return nil }

func printError(err error, printer *progress.Printer) error {
	if err == nil {
		return nil
	}

	printer.Pause()
	defer printer.Resume()

	for _, s := range errdefs.Sources(err) {
		s.Print(os.Stderr)
	}
	fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
	return nil
}
