package commands

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"sort"
	"strings"
	"sync"
	"text/tabwriter"

	"github.com/containerd/console"
	"github.com/containerd/platforms"
	"github.com/docker/buildx/bake"
	"github.com/docker/buildx/bake/hclparser"
	"github.com/docker/buildx/build"
	"github.com/docker/buildx/builder"
	"github.com/docker/buildx/controller/pb"
	"github.com/docker/buildx/localstate"
	"github.com/docker/buildx/util/buildflags"
	"github.com/docker/buildx/util/cobrautil/completion"
	"github.com/docker/buildx/util/confutil"
	"github.com/docker/buildx/util/desktop"
	"github.com/docker/buildx/util/dockerutil"
	"github.com/docker/buildx/util/osutil"
	"github.com/docker/buildx/util/progress"
	"github.com/docker/buildx/util/tracing"
	"github.com/docker/cli/cli/command"
	"github.com/moby/buildkit/identity"
	"github.com/moby/buildkit/util/progress/progressui"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/tonistiigi/go-csvvalue"
	"go.opentelemetry.io/otel/attribute"
)

type bakeOptions struct {
	files     []string
	overrides []string

	sbom       string
	provenance string
	allow      []string

	builder      string
	metadataFile string
	exportPush   bool
	exportLoad   bool
	callFunc     string

	print bool
	list  string

	// TODO: remove deprecated flags
	listTargets bool
	listVars    bool
}

func runBake(ctx context.Context, dockerCli command.Cli, targets []string, in bakeOptions, cFlags commonFlags) (err error) {
	mp := dockerCli.MeterProvider()

	ctx, end, err := tracing.TraceCurrentCommand(ctx, "bake")
	if err != nil {
		return err
	}
	defer func() {
		end(err)
	}()

	url, cmdContext, targets := bakeArgs(targets)
	if len(targets) == 0 {
		targets = []string{"default"}
	}

	callFunc, err := buildflags.ParseCallFunc(in.callFunc)
	if err != nil {
		return err
	}

	overrides := in.overrides
	if in.exportPush {
		overrides = append(overrides, "*.push=true")
	}
	if in.exportLoad {
		overrides = append(overrides, "*.load=true")
	}
	if callFunc != nil {
		overrides = append(overrides, fmt.Sprintf("*.call=%s", callFunc.Name))
	}
	if cFlags.noCache != nil {
		overrides = append(overrides, fmt.Sprintf("*.no-cache=%t", *cFlags.noCache))
	}
	if cFlags.pull != nil {
		overrides = append(overrides, fmt.Sprintf("*.pull=%t", *cFlags.pull))
	}
	if in.sbom != "" {
		overrides = append(overrides, fmt.Sprintf("*.attest=%s", buildflags.CanonicalizeAttest("sbom", in.sbom)))
	}
	if in.provenance != "" {
		overrides = append(overrides, fmt.Sprintf("*.attest=%s", buildflags.CanonicalizeAttest("provenance", in.provenance)))
	}
	contextPathHash, _ := os.Getwd()

	ent, err := bake.ParseEntitlements(in.allow)
	if err != nil {
		return err
	}
	wd, err := os.Getwd()
	if err != nil {
		return errors.Wrapf(err, "failed to get current working directory")
	}
	// filesystem access under the current working directory is allowed by default
	ent.FSRead = append(ent.FSRead, wd)
	ent.FSWrite = append(ent.FSWrite, wd)

	ctx2, cancel := context.WithCancelCause(context.TODO())
	defer cancel(errors.WithStack(context.Canceled))

	var nodes []builder.Node
	var progressConsoleDesc, progressTextDesc string

	if in.print && in.list != "" {
		return errors.New("--print and --list are mutually exclusive")
	}

	// instance only needed for reading remote bake files or building
	var driverType string
	if url != "" || !(in.print || in.list != "") {
		b, err := builder.New(dockerCli,
			builder.WithName(in.builder),
			builder.WithContextPathHash(contextPathHash),
		)
		if err != nil {
			return err
		}
		if err = updateLastActivity(dockerCli, b.NodeGroup); err != nil {
			return errors.Wrapf(err, "failed to update builder last activity time")
		}
		nodes, err = b.LoadNodes(ctx)
		if err != nil {
			return err
		}
		progressConsoleDesc = fmt.Sprintf("%s:%s", b.Driver, b.Name)
		progressTextDesc = fmt.Sprintf("building with %q instance using %s driver", b.Name, b.Driver)
		driverType = b.Driver
	}

	var term bool
	if _, err := console.ConsoleFromFile(os.Stderr); err == nil {
		term = true
	}
	attributes := bakeMetricAttributes(dockerCli, driverType, url, cmdContext, targets, &in)

	progressMode := progressui.DisplayMode(cFlags.progress)
	var printer *progress.Printer

	makePrinter := func() error {
		var err error
		printer, err = progress.NewPrinter(ctx2, os.Stderr, progressMode,
			progress.WithDesc(progressTextDesc, progressConsoleDesc),
			progress.WithMetrics(mp, attributes),
			progress.WithOnClose(func() {
				printWarnings(os.Stderr, printer.Warnings(), progressMode)
			}),
		)
		return err
	}

	if err := makePrinter(); err != nil {
		return err
	}

	files, inp, err := readBakeFiles(ctx, nodes, url, in.files, dockerCli.In(), printer)
	if err != nil {
		return err
	}

	if len(files) == 0 {
		return errors.New("couldn't find a bake definition")
	}

	defaults := map[string]string{
		// don't forget to update documentation if you add a new
		// built-in variable: docs/bake-reference.md#built-in-variables
		"BAKE_CMD_CONTEXT":    cmdContext,
		"BAKE_LOCAL_PLATFORM": platforms.Format(platforms.DefaultSpec()),
	}

	if in.list != "" {
		cfg, pm, err := bake.ParseFiles(files, defaults)
		if err != nil {
			return err
		}
		if err = printer.Wait(); err != nil {
			return err
		}
		list, err := parseList(in.list)
		if err != nil {
			return err
		}
		switch list.Type {
		case "targets":
			return printTargetList(dockerCli.Out(), list.Format, cfg)
		case "variables":
			return printVars(dockerCli.Out(), list.Format, pm.AllVariables)
		}
	}

	tgts, grps, err := bake.ReadTargets(ctx, files, targets, overrides, defaults, &ent)
	if err != nil {
		return err
	}

	if v := os.Getenv("SOURCE_DATE_EPOCH"); v != "" {
		// TODO: extract env var parsing to a method easily usable by library consumers
		for _, t := range tgts {
			if _, ok := t.Args["SOURCE_DATE_EPOCH"]; ok {
				continue
			}
			if t.Args == nil {
				t.Args = map[string]*string{}
			}
			t.Args["SOURCE_DATE_EPOCH"] = &v
		}
	}

	// this function can update target context string from the input so call before printOnly check
	bo, err := bake.TargetsToBuildOpt(tgts, inp)
	if err != nil {
		return err
	}

	def := struct {
		Group  map[string]*bake.Group  `json:"group,omitempty"`
		Target map[string]*bake.Target `json:"target"`
	}{
		Group:  grps,
		Target: tgts,
	}

	if in.print {
		if err = printer.Wait(); err != nil {
			return err
		}
		dtdef, err := json.MarshalIndent(def, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(dockerCli.Out(), string(dtdef))
		return err
	}

	for _, opt := range bo {
		if opt.CallFunc != nil {
			cf, err := buildflags.ParseCallFunc(opt.CallFunc.Name)
			if err != nil {
				return err
			}
			opt.CallFunc.Name = cf.Name
		}
	}

	exp, err := ent.Validate(bo)
	if err != nil {
		return err
	}
	if err := exp.Prompt(ctx, url != "", &syncWriter{w: dockerCli.Err(), wait: printer.Wait}); err != nil {
		return err
	}
	if printer.IsDone() {
		// init new printer as old one was stopped to show the prompt
		if err := makePrinter(); err != nil {
			return err
		}
	}

	if err := saveLocalStateGroup(dockerCli, in, targets, bo, overrides, def); err != nil {
		return err
	}

	done := timeBuildCommand(mp, attributes)
	resp, retErr := build.Build(ctx, nodes, bo, dockerutil.NewClient(dockerCli), confutil.NewConfig(dockerCli), printer)
	if err := printer.Wait(); retErr == nil {
		retErr = err
	}
	if retErr != nil {
		err = wrapBuildError(retErr, true)
	}
	done(err)

	if err != nil {
		return err
	}

	if progressMode != progressui.QuietMode && progressMode != progressui.RawJSONMode {
		desktop.PrintBuildDetails(os.Stderr, printer.BuildRefs(), term)
	}
	if len(in.metadataFile) > 0 {
		dt := make(map[string]interface{})
		for t, r := range resp {
			dt[t] = decodeExporterResponse(r.ExporterResponse)
		}
		if callFunc == nil {
			if warnings := printer.Warnings(); len(warnings) > 0 && confutil.MetadataWarningsEnabled() {
				dt["buildx.build.warnings"] = warnings
			}
		}
		if err := writeMetadataFile(in.metadataFile, dt); err != nil {
			return err
		}
	}

	var callFormatJSON bool
	jsonResults := map[string]map[string]any{}
	if callFunc != nil {
		callFormatJSON = callFunc.Format == "json"
	}
	var sep bool
	var exitCode int

	names := make([]string, 0, len(bo))
	for name := range bo {
		names = append(names, name)
	}
	slices.Sort(names)

	for _, name := range names {
		req := bo[name]
		if req.CallFunc == nil {
			continue
		}

		pf := &pb.CallFunc{
			Name:         req.CallFunc.Name,
			Format:       req.CallFunc.Format,
			IgnoreStatus: req.CallFunc.IgnoreStatus,
		}

		if callFunc != nil {
			pf.Format = callFunc.Format
			pf.IgnoreStatus = callFunc.IgnoreStatus
		}

		var res map[string]string
		if sp, ok := resp[name]; ok {
			res = sp.ExporterResponse
		}

		if callFormatJSON {
			jsonResults[name] = map[string]any{}
			buf := &bytes.Buffer{}
			if code, err := printResult(buf, pf, res, name, &req.Inputs); err != nil {
				jsonResults[name]["error"] = err.Error()
				exitCode = 1
			} else if code != 0 && exitCode == 0 {
				exitCode = code
			}
			m := map[string]*json.RawMessage{}
			if err := json.Unmarshal(buf.Bytes(), &m); err == nil {
				for k, v := range m {
					jsonResults[name][k] = v
				}
			} else {
				jsonResults[name][pf.Name] = json.RawMessage(buf.Bytes())
			}
		} else {
			if sep {
				fmt.Fprintln(dockerCli.Out())
			} else {
				sep = true
			}
			fmt.Fprintf(dockerCli.Out(), "%s\n", name)
			if descr := tgts[name].Description; descr != "" {
				fmt.Fprintf(dockerCli.Out(), "%s\n", descr)
			}

			fmt.Fprintln(dockerCli.Out())
			if code, err := printResult(dockerCli.Out(), pf, res, name, &req.Inputs); err != nil {
				fmt.Fprintf(dockerCli.Out(), "error: %v\n", err)
				exitCode = 1
			} else if code != 0 && exitCode == 0 {
				exitCode = code
			}
		}
	}
	if callFormatJSON {
		out := struct {
			Group  map[string]*bake.Group    `json:"group,omitempty"`
			Target map[string]map[string]any `json:"target"`
		}{
			Group:  grps,
			Target: map[string]map[string]any{},
		}

		for name, def := range tgts {
			out.Target[name] = map[string]any{
				"build": def,
			}
			if res, ok := jsonResults[name]; ok {
				printName := bo[name].CallFunc.Name
				if printName == "lint" {
					printName = "check"
				}
				out.Target[name][printName] = res
			}
		}
		dt, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(dockerCli.Out(), string(dt))
	}

	if exitCode != 0 {
		os.Exit(exitCode)
	}

	return nil
}

func bakeCmd(dockerCli command.Cli, rootOpts *rootOptions) *cobra.Command {
	var options bakeOptions
	var cFlags commonFlags

	cmd := &cobra.Command{
		Use:     "bake [OPTIONS] [TARGET...]",
		Aliases: []string{"f"},
		Short:   "Build from a file",
		RunE: func(cmd *cobra.Command, args []string) error {
			// reset to nil to avoid override is unset
			if !cmd.Flags().Lookup("no-cache").Changed {
				cFlags.noCache = nil
			}
			if !cmd.Flags().Lookup("pull").Changed {
				cFlags.pull = nil
			}
			if options.list == "" {
				if options.listTargets {
					options.list = "targets"
				} else if options.listVars {
					options.list = "variables"
				}
			}
			options.builder = rootOpts.builder
			options.metadataFile = cFlags.metadataFile
			// Other common flags (noCache, pull and progress) are processed in runBake function.
			return runBake(cmd.Context(), dockerCli, args, options, cFlags)
		},
		ValidArgsFunction: completion.BakeTargets(options.files),
	}

	flags := cmd.Flags()

	flags.StringArrayVarP(&options.files, "file", "f", []string{}, "Build definition file")
	flags.BoolVar(&options.exportLoad, "load", false, `Shorthand for "--set=*.output=type=docker"`)
	flags.BoolVar(&options.exportPush, "push", false, `Shorthand for "--set=*.output=type=registry"`)
	flags.StringVar(&options.sbom, "sbom", "", `Shorthand for "--set=*.attest=type=sbom"`)
	flags.StringVar(&options.provenance, "provenance", "", `Shorthand for "--set=*.attest=type=provenance"`)
	flags.StringArrayVar(&options.overrides, "set", nil, `Override target value (e.g., "targetpattern.key=value")`)
	flags.StringVar(&options.callFunc, "call", "build", `Set method for evaluating build ("check", "outline", "targets")`)
	flags.StringArrayVar(&options.allow, "allow", nil, "Allow build to access specified resources")

	flags.VarPF(callAlias(&options.callFunc, "check"), "check", "", `Shorthand for "--call=check"`)
	flags.Lookup("check").NoOptDefVal = "true"

	flags.BoolVar(&options.print, "print", false, "Print the options without building")
	flags.StringVar(&options.list, "list", "", "List targets or variables")

	// TODO: remove deprecated flags
	flags.BoolVar(&options.listTargets, "list-targets", false, "List available targets")
	flags.MarkHidden("list-targets")
	flags.MarkDeprecated("list-targets", "list-targets is deprecated, use list=targets instead")
	flags.BoolVar(&options.listVars, "list-variables", false, "List defined variables")
	flags.MarkHidden("list-variables")
	flags.MarkDeprecated("list-variables", "list-variables is deprecated, use list=variables instead")

	commonBuildFlags(&cFlags, flags)

	return cmd
}

func saveLocalStateGroup(dockerCli command.Cli, in bakeOptions, targets []string, bo map[string]build.Options, overrides []string, def any) error {
	prm := confutil.MetadataProvenance()
	if len(in.metadataFile) == 0 {
		prm = confutil.MetadataProvenanceModeDisabled
	}
	groupRef := identity.NewID()
	refs := make([]string, 0, len(bo))
	for k, b := range bo {
		if b.CallFunc != nil {
			continue
		}
		b.Ref = identity.NewID()
		b.GroupRef = groupRef
		b.ProvenanceResponseMode = prm
		refs = append(refs, b.Ref)
		bo[k] = b
	}
	if len(refs) == 0 {
		return nil
	}
	l, err := localstate.New(confutil.NewConfig(dockerCli))
	if err != nil {
		return err
	}
	dtdef, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		return err
	}
	return l.SaveGroup(groupRef, localstate.StateGroup{
		Definition: dtdef,
		Targets:    targets,
		Inputs:     overrides,
		Refs:       refs,
	})
}

// bakeArgs will retrieve the remote url, command context, and targets
// from the command line arguments.
func bakeArgs(args []string) (url, cmdContext string, targets []string) {
	cmdContext, targets = "cwd://", args
	if len(targets) == 0 || !build.IsRemoteURL(targets[0]) {
		return url, cmdContext, targets
	}
	url, targets = targets[0], targets[1:]
	if len(targets) == 0 || !build.IsRemoteURL(targets[0]) {
		return url, cmdContext, targets
	}
	cmdContext, targets = targets[0], targets[1:]
	return url, cmdContext, targets
}

func readBakeFiles(ctx context.Context, nodes []builder.Node, url string, names []string, stdin io.Reader, pw progress.Writer) (files []bake.File, inp *bake.Input, err error) {
	var lnames []string // local
	var rnames []string // remote
	var anames []string // both
	for _, v := range names {
		if strings.HasPrefix(v, "cwd://") {
			tname := strings.TrimPrefix(v, "cwd://")
			lnames = append(lnames, tname)
			anames = append(anames, tname)
		} else {
			rnames = append(rnames, v)
			anames = append(anames, v)
		}
	}

	if url != "" {
		var rfiles []bake.File
		rfiles, inp, err = bake.ReadRemoteFiles(ctx, nodes, url, rnames, pw)
		if err != nil {
			return nil, nil, err
		}
		files = append(files, rfiles...)
	}

	if len(lnames) > 0 || url == "" {
		var lfiles []bake.File
		progress.Wrap("[internal] load local bake definitions", pw.Write, func(sub progress.SubLogger) error {
			if url != "" {
				lfiles, err = bake.ReadLocalFiles(lnames, stdin, sub)
			} else {
				lfiles, err = bake.ReadLocalFiles(anames, stdin, sub)
			}
			return nil
		})
		if err != nil {
			return nil, nil, err
		}
		files = append(files, lfiles...)
	}

	return
}

type listEntry struct {
	Type   string
	Format string
}

func parseList(input string) (listEntry, error) {
	res := listEntry{}

	fields, err := csvvalue.Fields(input, nil)
	if err != nil {
		return res, err
	}

	if len(fields) == 1 && fields[0] == input && !strings.HasPrefix(input, "type=") {
		res.Type = input
	}

	if res.Type == "" {
		for _, field := range fields {
			key, value, ok := strings.Cut(field, "=")
			if !ok {
				return res, errors.Errorf("invalid value %s", field)
			}
			key = strings.TrimSpace(strings.ToLower(key))
			switch key {
			case "type":
				res.Type = value
			case "format":
				res.Format = value
			default:
				return res, errors.Errorf("unexpected key '%s' in '%s'", key, field)
			}
		}
	}
	if res.Format == "" {
		res.Format = "table"
	}

	switch res.Type {
	case "targets", "variables":
	default:
		return res, errors.Errorf("invalid list type %q", res.Type)
	}

	switch res.Format {
	case "table", "json":
	default:
		return res, errors.Errorf("invalid list format %q", res.Format)
	}

	return res, nil
}

func printVars(w io.Writer, format string, vars []*hclparser.Variable) error {
	slices.SortFunc(vars, func(a, b *hclparser.Variable) int {
		return cmp.Compare(a.Name, b.Name)
	})

	if format == "json" {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(vars)
	}

	tw := tabwriter.NewWriter(w, 1, 8, 1, '\t', 0)
	defer tw.Flush()

	tw.Write([]byte("VARIABLE\tVALUE\tDESCRIPTION\n"))

	for _, v := range vars {
		var value string
		if v.Value != nil {
			value = *v.Value
		} else {
			value = "<null>"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", v.Name, value, v.Description)
	}
	return nil
}

func printTargetList(w io.Writer, format string, cfg *bake.Config) error {
	type targetOrGroup struct {
		name   string
		target *bake.Target
		group  *bake.Group
	}

	list := make([]targetOrGroup, 0, len(cfg.Targets)+len(cfg.Groups))
	for _, tgt := range cfg.Targets {
		list = append(list, targetOrGroup{name: tgt.Name, target: tgt})
	}
	for _, grp := range cfg.Groups {
		list = append(list, targetOrGroup{name: grp.Name, group: grp})
	}

	slices.SortFunc(list, func(a, b targetOrGroup) int {
		return cmp.Compare(a.name, b.name)
	})

	var tw *tabwriter.Writer
	if format == "table" {
		tw = tabwriter.NewWriter(w, 1, 8, 1, '\t', 0)
		defer tw.Flush()
		tw.Write([]byte("TARGET\tDESCRIPTION\n"))
	}

	type targetList struct {
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
		Group       bool   `json:"group,omitempty"`
	}
	var targetsList []targetList

	for _, tgt := range list {
		if strings.HasPrefix(tgt.name, "_") {
			// convention for a private target
			continue
		}
		var descr string
		if tgt.target != nil {
			descr = tgt.target.Description
			targetsList = append(targetsList, targetList{Name: tgt.name, Description: descr})
		} else if tgt.group != nil {
			descr = tgt.group.Description
			if len(tgt.group.Targets) > 0 {
				slices.Sort(tgt.group.Targets)
				names := strings.Join(tgt.group.Targets, ", ")
				if descr != "" {
					descr += " (" + names + ")"
				} else {
					descr = names
				}
			}
			targetsList = append(targetsList, targetList{Name: tgt.name, Description: descr, Group: true})
		}
		if format == "table" {
			fmt.Fprintf(tw, "%s\t%s\n", tgt.name, descr)
		}
	}

	if format == "json" {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(targetsList)
	}

	return nil
}

func bakeMetricAttributes(dockerCli command.Cli, driverType, url, cmdContext string, targets []string, options *bakeOptions) attribute.Set {
	return attribute.NewSet(
		commandNameAttribute.String("bake"),
		attribute.Stringer(string(commandOptionsHash), &bakeOptionsHash{
			bakeOptions: options,
			cfg:         confutil.NewConfig(dockerCli),
			url:         url,
			cmdContext:  cmdContext,
			targets:     targets,
		}),
		driverNameAttribute.String(options.builder),
		driverTypeAttribute.String(driverType),
	)
}

type bakeOptionsHash struct {
	*bakeOptions
	cfg        *confutil.Config
	url        string
	cmdContext string
	targets    []string
	result     string
	resultOnce sync.Once
}

func (o *bakeOptionsHash) String() string {
	o.resultOnce.Do(func() {
		url := o.url
		cmdContext := o.cmdContext
		if cmdContext == "cwd://" {
			// Resolve the directory if the cmdContext is the current working directory.
			cmdContext = osutil.GetWd()
		}

		// Sort the inputs for files and targets since the ordering
		// doesn't matter, but avoid modifying the original slice.
		files := immutableSort(o.files)
		targets := immutableSort(o.targets)

		joinedFiles := strings.Join(files, ",")
		joinedTargets := strings.Join(targets, ",")
		salt := o.cfg.TryNodeIdentifier()

		h := sha256.New()
		for _, s := range []string{url, cmdContext, joinedFiles, joinedTargets, salt} {
			_, _ = io.WriteString(h, s)
			h.Write([]byte{0})
		}
		o.result = hex.EncodeToString(h.Sum(nil))
	})
	return o.result
}

// immutableSort will sort the entries in s without modifying the original slice.
func immutableSort(s []string) []string {
	if !sort.StringsAreSorted(s) {
		cpy := make([]string, len(s))
		copy(cpy, s)
		sort.Strings(cpy)
		return cpy
	}
	return s
}

type syncWriter struct {
	w    io.Writer
	once sync.Once
	wait func() error
}

func (w *syncWriter) Write(p []byte) (n int, err error) {
	w.once.Do(func() {
		if w.wait != nil {
			err = w.wait()
		}
	})
	if err != nil {
		return 0, err
	}
	return w.w.Write(p)
}
