package commands

import (
	"encoding/json"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/docker/buildx/build"
	"github.com/docker/buildx/monitor"
	"github.com/docker/buildx/util/cobrautil"
	"github.com/docker/buildx/util/ioset"
	"github.com/docker/buildx/util/progress"
	"github.com/docker/cli/cli/command"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/tonistiigi/go-csvvalue"
)

type debugOptions struct {
	// InvokeFlag is a flag to configure the launched debugger and the commaned executed on the debugger.
	InvokeFlag string

	// OnFlag is a flag to configure the timing of launching the debugger.
	OnFlag string
}

type debuggerInfo struct {
	Name      string
	UserAgent string
}

// debuggerOptions will start a debuggerOptions instance.
type debuggerOptions interface {
	New(in ioset.In) (debuggerInstance, error)
	Info() debuggerInfo
}

// debuggerInstance is an instance of a Debugger that has been started.
type debuggerInstance interface {
	Start(printer *progress.Printer, opts *BuildOptions) error
	Handler() build.Handler
	Stop() error
	Out() io.Writer
}

func debugCmd(dockerCli command.Cli, rootOpts *rootOptions) *cobra.Command {
	var options debugOptions
	cmd := &cobra.Command{
		Use:   "debug",
		Short: "Start debugger",

		DisableFlagsInUseLine: true,
	}
	cobrautil.MarkCommandExperimental(cmd)

	flags := cmd.Flags()
	flags.StringVar(&options.InvokeFlag, "invoke", "", "Launch a monitor with executing specified command")
	flags.StringVar(&options.OnFlag, "on", "error", "When to launch the monitor ([always, error])")

	cobrautil.MarkFlagsExperimental(flags, "invoke", "on")

	cmd.AddCommand(buildCmd(dockerCli, rootOpts, &options))
	return cmd
}

func (d *debugOptions) New(in ioset.In) (debuggerInstance, error) {
	cfg, err := parseInvokeConfig(d.InvokeFlag, d.OnFlag)
	if err != nil {
		return nil, err
	}

	return &monitorDebuggerInstance{
		cfg: cfg,
		in:  in.Stdin,
	}, nil
}

func (d *debugOptions) Info() debuggerInfo {
	return debuggerInfo{
		Name: "debug",
	}
}

type monitorDebuggerInstance struct {
	cfg *build.InvokeConfig
	in  io.ReadCloser
	m   *monitor.Monitor
}

func (d *monitorDebuggerInstance) Start(printer *progress.Printer, opts *BuildOptions) error {
	d.m = monitor.New(d.cfg, d.in, os.Stdout, os.Stderr, printer)
	return nil
}

func (d *monitorDebuggerInstance) Handler() build.Handler {
	return d.m.Handler()
}

func (d *monitorDebuggerInstance) Stop() error {
	return d.m.Close()
}

func (d *monitorDebuggerInstance) Out() io.Writer {
	return os.Stderr
}

func parseInvokeConfig(invoke, on string) (*build.InvokeConfig, error) {
	cfg := &build.InvokeConfig{}
	switch on {
	case "always":
		cfg.SuspendOn = build.SuspendAlways
	case "error":
		cfg.SuspendOn = build.SuspendError
	default:
		if invoke != "" {
			cfg.SuspendOn = build.SuspendAlways
		}
	}

	cfg.Tty = true
	cfg.NoCmd = true
	switch invoke {
	case "default", "":
		return cfg, nil
	case "on-error":
		// NOTE: we overwrite the command to run because the original one should fail on the failed step.
		// TODO: make this configurable via flags or restorable from LLB.
		// Discussion: https://github.com/docker/buildx/pull/1640#discussion_r1113295900
		cfg.Cmd = []string{"/bin/sh"}
		cfg.NoCmd = false
		return cfg, nil
	}

	csvParser := csvvalue.NewParser()
	csvParser.LazyQuotes = true
	fields, err := csvParser.Fields(invoke, nil)
	if err != nil {
		return nil, err
	}
	if len(fields) == 1 && !strings.Contains(fields[0], "=") {
		cfg.Cmd = []string{fields[0]}
		cfg.NoCmd = false
		return cfg, nil
	}
	cfg.NoUser = true
	cfg.NoCwd = true
	for _, field := range fields {
		parts := strings.SplitN(field, "=", 2)
		if len(parts) != 2 {
			return nil, errors.Errorf("invalid value %s", field)
		}
		key := strings.ToLower(parts[0])
		value := parts[1]
		switch key {
		case "args":
			cfg.Cmd = append(cfg.Cmd, maybeJSONArray(value)...)
			cfg.NoCmd = false
		case "entrypoint":
			cfg.Entrypoint = append(cfg.Entrypoint, maybeJSONArray(value)...)
			if cfg.Cmd == nil {
				cfg.Cmd = []string{}
				cfg.NoCmd = false
			}
		case "env":
			cfg.Env = append(cfg.Env, maybeJSONArray(value)...)
		case "user":
			cfg.User = value
			cfg.NoUser = false
		case "cwd":
			cfg.Cwd = value
			cfg.NoCwd = false
		case "tty":
			cfg.Tty, err = strconv.ParseBool(value)
			if err != nil {
				return nil, errors.Errorf("failed to parse tty: %v", err)
			}
		default:
			return nil, errors.Errorf("unknown key %q", key)
		}
	}
	return cfg, nil
}

func maybeJSONArray(v string) []string {
	var list []string
	if err := json.Unmarshal([]byte(v), &list); err == nil {
		return list
	}
	return []string{v}
}
