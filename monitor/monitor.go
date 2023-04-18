package monitor

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"text/tabwriter"

	"github.com/containerd/console"
	"github.com/docker/buildx/controller/control"
	controllererrors "github.com/docker/buildx/controller/errdefs"
	controllerapi "github.com/docker/buildx/controller/pb"
	"github.com/docker/buildx/util/ioset"
	"github.com/moby/buildkit/identity"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"golang.org/x/term"
)

const helpMessage = `
Available commands are:
  reload     reloads the context and build it.
  rollback   re-runs the interactive container with initial rootfs contents.
  list       list buildx sessions.
  attach     attach to a buildx server or a process in the container.
  exec       execute a process in the interactive container.
  ps         list processes invoked by "exec". Use "attach" to attach IO to that process.
  disconnect disconnect a client from a buildx server. Specific session ID can be specified an arg.
  kill       kill buildx server.
  exit       exits monitor.
  help       shows this message.
`

// RunMonitor provides an interactive session for running and managing containers via specified IO.
func RunMonitor(ctx context.Context, curRef string, options *controllerapi.BuildOptions, invokeConfig controllerapi.InvokeConfig, c control.BuildxController, progressMode string, stdin io.ReadCloser, stdout io.WriteCloser, stderr console.File) error {
	defer func() {
		if err := c.Disconnect(ctx, curRef); err != nil {
			logrus.Warnf("disconnect error: %v", err)
		}
	}()
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
		invokeIO: invokeForwarder,
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
		invokeFunc: c.Invoke,
	}

	// Start container automatically
	fmt.Fprintf(stdout, "Launching interactive container. Press Ctrl-a-c to switch to monitor console\n")
	invokeConfig.Rollback = false
	invokeConfig.Initial = false
	id := m.rollback(ctx, curRef, invokeConfig)
	fmt.Fprintf(stdout, "Interactive container was restarted with process %q. Press Ctrl-a-c to switch to the new container\n", id)

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
				args := strings.Fields(l) // TODO: use shlex
				if len(args) == 0 {
					continue
				}
				switch args[0] {
				case "":
					// nop
				case "reload":
					var bo *controllerapi.BuildOptions
					if curRef != "" {
						// Rebuilding an existing session; Restore the build option used for building this session.
						res, err := c.Inspect(ctx, curRef)
						if err != nil {
							fmt.Printf("failed to inspect the current build session: %v\n", err)
						} else {
							bo = res.Options
						}
					} else {
						bo = options
					}
					if bo == nil {
						fmt.Println("reload: no build option is provided")
						continue
					}
					if curRef != "" {
						if err := c.Disconnect(ctx, curRef); err != nil {
							fmt.Println("disconnect error", err)
						}
					}
					var resultUpdated bool
					ref, _, err := c.Build(ctx, *bo, nil, stdout, stderr, progressMode) // TODO: support stdin, hold build ref
					if err != nil {
						var be *controllererrors.BuildError
						if errors.As(err, &be) {
							curRef = be.Ref
							resultUpdated = true
						} else {
							fmt.Printf("failed to reload: %v\n", err)
						}
					} else {
						curRef = ref
						resultUpdated = true
					}
					if resultUpdated {
						// rollback the running container with the new result
						id := m.rollback(ctx, curRef, invokeConfig)
						fmt.Fprintf(stdout, "Interactive container was restarted with process %q. Press Ctrl-a-c to switch to the new container\n", id)
					}
				case "rollback":
					cfg := invokeConfig
					if len(args) >= 2 {
						cmds := args[1:]
						if cmds[0] == "--init" {
							cfg.Initial = true
							cmds = cmds[1:]
						}
						if len(cmds) > 0 {
							cfg.Entrypoint = []string{cmds[0]}
							cfg.Cmd = cmds[1:]
						}
					}
					id := m.rollback(ctx, curRef, cfg)
					fmt.Fprintf(stdout, "Interactive container was restarted with process %q. Press Ctrl-a-c to switch to the new container\n", id)
				case "list":
					refs, err := c.List(ctx)
					if err != nil {
						fmt.Printf("failed to list: %v\n", err)
					}
					sort.Strings(refs)
					tw := tabwriter.NewWriter(stdout, 1, 8, 1, '\t', 0)
					fmt.Fprintln(tw, "ID\tCURRENT_SESSION")
					for _, k := range refs {
						fmt.Fprintf(tw, "%-20s\t%v\n", k, k == curRef)
					}
					tw.Flush()
				case "disconnect":
					target := curRef
					if len(args) >= 2 {
						target = args[1]
					}
					isProcess, err := isProcessID(ctx, c, curRef, target)
					if err == nil && isProcess {
						if err := c.DisconnectProcess(ctx, curRef, target); err != nil {
							fmt.Printf("disconnect process failed %v\n", target)
							continue
						}
						continue
					}
					if err := c.Disconnect(ctx, target); err != nil {
						fmt.Println("disconnect error", err)
					}
				case "kill":
					if err := c.Kill(ctx); err != nil {
						fmt.Printf("failed to kill: %v\n", err)
					}
				case "attach":
					if len(args) < 2 {
						fmt.Println("attach: server name must be passed")
						continue
					}
					ref := args[1]
					var id string

					isProcess, err := isProcessID(ctx, c, curRef, ref)
					if err == nil && isProcess {
						m.attach(ctx, curRef, ref)
						id = ref
					}
					if id == "" {
						refs, err := c.List(ctx)
						if err != nil {
							fmt.Printf("failed to get the list of sessions: %v\n", err)
							continue
						}
						found := false
						for _, s := range refs {
							if s == ref {
								found = true
								break
							}
						}
						if !found {
							fmt.Printf("unknown ID: %q\n", ref)
							continue
						}
						if m.invokeCancel != nil {
							m.invokeCancel() // Finish existing attach
						}
						curRef = ref
					}
					fmt.Fprintf(stdout, "Attached to process %q. Press Ctrl-a-c to switch to the new container\n", id)
				case "exec":
					if len(args) < 2 {
						fmt.Println("exec: server name must be passed")
						continue
					}
					if curRef == "" {
						fmt.Println("attach to a session first")
						continue
					}
					cfg := controllerapi.InvokeConfig{
						Entrypoint: []string{args[1]},
						Cmd:        args[2:],
						// TODO: support other options as well via flags
						Env:  invokeConfig.Env,
						User: invokeConfig.User,
						Cwd:  invokeConfig.Cwd,
						Tty:  true,
					}
					pid := m.exec(ctx, curRef, cfg)
					fmt.Fprintf(stdout, "Process %q started. Press Ctrl-a-c to switch to that process.\n", pid)
				case "ps":
					plist, err := c.ListProcesses(ctx, curRef)
					if err != nil {
						fmt.Println("cannot list process:", err)
						continue
					}
					tw := tabwriter.NewWriter(stdout, 1, 8, 1, '\t', 0)
					fmt.Fprintln(tw, "PID\tCURRENT_SESSION\tCOMMAND")
					for _, p := range plist {
						fmt.Fprintf(tw, "%-20s\t%v\t%v\n", p.ProcessID, p.ProcessID == m.attachedPid.Load(), append(p.InvokeConfig.Entrypoint, p.InvokeConfig.Cmd...))
					}
					tw.Flush()
				case "exit":
					return
				case "help":
					fmt.Fprint(stdout, helpMessage)
				default:
					fmt.Printf("unknown command: %q\n", l)
					fmt.Fprint(stdout, helpMessage)
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

type readWriter struct {
	io.Reader
	io.Writer
}

type monitor struct {
	muxIO        *ioset.MuxIO
	invokeIO     *ioset.Forwarder
	invokeFunc   func(ctx context.Context, ref, pid string, cfg controllerapi.InvokeConfig, in io.ReadCloser, out io.WriteCloser, err io.WriteCloser) error
	invokeCancel func()
	attachedPid  atomic.Value
}

func (m *monitor) rollback(ctx context.Context, ref string, cfg controllerapi.InvokeConfig) string {
	pid := identity.NewID()
	cfg1 := cfg
	cfg1.Rollback = true
	return m.startInvoke(ctx, ref, pid, cfg1)
}

func (m *monitor) exec(ctx context.Context, ref string, cfg controllerapi.InvokeConfig) string {
	return m.startInvoke(ctx, ref, identity.NewID(), cfg)
}

func (m *monitor) attach(ctx context.Context, ref, pid string) {
	m.startInvoke(ctx, ref, pid, controllerapi.InvokeConfig{})
}

func (m *monitor) close() {
	if m.invokeCancel != nil {
		m.invokeCancel()
	}
}

func (m *monitor) startInvoke(ctx context.Context, ref, pid string, cfg controllerapi.InvokeConfig) string {
	if m.invokeCancel != nil {
		m.invokeCancel() // Finish existing attach
	}
	if len(cfg.Entrypoint) == 0 && len(cfg.Cmd) == 0 {
		cfg.Entrypoint = []string{"sh"} // launch shell by default
	}
	go func() {
		// Start a new invoke
		if err := m.invoke(ctx, ref, pid, cfg); err != nil {
			logrus.Debugf("invoke error: %v", err)
		}
		if pid == m.attachedPid.Load() {
			m.attachedPid.Store("")
		}
	}()
	m.attachedPid.Store(pid)
	return pid
}

func (m *monitor) invoke(ctx context.Context, ref, pid string, cfg controllerapi.InvokeConfig) error {
	m.muxIO.Enable(1)
	defer m.muxIO.Disable(1)
	if err := m.muxIO.SwitchTo(1); err != nil {
		return errors.Errorf("failed to switch to process IO: %v", err)
	}
	if ref == "" {
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

	err := m.invokeFunc(invokeCtx, ref, pid, cfg, containerIn.Stdin, containerIn.Stdout, containerIn.Stderr)
	close(waitInvokeDoneCh)

	return err
}

type nopCloser struct {
	io.Writer
}

func (c nopCloser) Close() error { return nil }

func isProcessID(ctx context.Context, c control.BuildxController, curRef, ref string) (bool, error) {
	infos, err := c.ListProcesses(ctx, curRef)
	if err != nil {
		return false, err
	}
	for _, p := range infos {
		if p.ProcessID == ref {
			return true, nil
		}
	}
	return false, nil
}
