package commands

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
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
	"github.com/docker/buildx/util/progress"
	"github.com/docker/buildx/util/tracing"
	"github.com/docker/cli/cli/command"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/identity"
	"github.com/moby/buildkit/util/progress/progressui"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

type bakeOptions struct {
	files       []string
	overrides   []string
	printOnly   bool
	listTargets bool
	listVars    bool
	sbom        string
	provenance  string

	builder      string
	metadataFile string
	exportPush   bool
	exportLoad   bool
	callFunc     string
}

func runBake(ctx context.Context, dockerCli command.Cli, targets []string, in bakeOptions, cFlags commonFlags) (err error) {
	ctx, end, err := tracing.TraceCurrentCommand(ctx, "bake")
	if err != nil {
		return err
	}
	defer func() {
		end(err)
	}()

	var url string
	cmdContext := "cwd://"

	if len(targets) > 0 {
		if build.IsRemoteURL(targets[0]) {
			url = targets[0]
			targets = targets[1:]
			if len(targets) > 0 {
				if build.IsRemoteURL(targets[0]) {
					cmdContext = targets[0]
					targets = targets[1:]
				}
			}
		}
	}

	if len(targets) == 0 {
		targets = []string{"default"}
	}

	callFunc, err := buildflags.ParsePrintFunc(in.callFunc)
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

	ctx2, cancel := context.WithCancel(context.TODO())
	defer cancel()

	var nodes []builder.Node
	var progressConsoleDesc, progressTextDesc string

	// instance only needed for reading remote bake files or building
	if url != "" || !in.printOnly {
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
	}

	var term bool
	if _, err := console.ConsoleFromFile(os.Stderr); err == nil {
		term = true
	}

	progressMode := progressui.DisplayMode(cFlags.progress)
	var printer *progress.Printer
	printer, err = progress.NewPrinter(ctx2, os.Stderr, progressMode,
		progress.WithDesc(progressTextDesc, progressConsoleDesc),
		progress.WithOnClose(func() {
			printWarnings(os.Stderr, printer.Warnings(), progressMode)
		}),
	)
	if err != nil {
		return err
	}

	var resp map[string]*client.SolveResponse

	defer func() {
		if printer != nil {
			err1 := printer.Wait()
			if err == nil {
				err = err1
			}
			if err != nil {
				return
			}
			if progressMode != progressui.QuietMode && progressMode != progressui.RawJSONMode {
				desktop.PrintBuildDetails(os.Stderr, printer.BuildRefs(), term)
			}
			if resp != nil && len(in.metadataFile) > 0 {
				dt := make(map[string]interface{})
				for t, r := range resp {
					dt[t] = decodeExporterResponse(r.ExporterResponse)
				}
				if warnings := printer.Warnings(); len(warnings) > 0 && confutil.MetadataWarningsEnabled() {
					dt["buildx.build.warnings"] = warnings
				}
				err = writeMetadataFile(in.metadataFile, dt)
			}
		}
	}()

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

	if in.listTargets || in.listVars {
		cfg, pm, err := bake.ParseFiles(files, defaults)
		if err != nil {
			return err
		}

		err = printer.Wait()
		printer = nil
		if err != nil {
			return err
		}
		if in.listTargets {
			return printTargetList(dockerCli.Out(), cfg)
		} else if in.listVars {
			return printVars(dockerCli.Out(), pm.AllVariables)
		}
	}

	tgts, grps, err := bake.ReadTargets(ctx, files, targets, overrides, defaults)
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

	if in.printOnly {
		dt, err := json.MarshalIndent(def, "", "  ")
		if err != nil {
			return err
		}
		err = printer.Wait()
		printer = nil
		if err != nil {
			return err
		}
		fmt.Fprintln(dockerCli.Out(), string(dt))
		return nil
	}

	for _, opt := range bo {
		if opt.PrintFunc != nil {
			cf, err := buildflags.ParsePrintFunc(opt.PrintFunc.Name)
			if err != nil {
				return err
			}
			opt.PrintFunc.Name = cf.Name
		}
	}

	prm := confutil.MetadataProvenance()
	if len(in.metadataFile) == 0 {
		prm = confutil.MetadataProvenanceModeDisabled
	}

	groupRef := identity.NewID()
	var refs []string
	for k, b := range bo {
		b.Ref = identity.NewID()
		b.GroupRef = groupRef
		b.ProvenanceResponseMode = prm
		refs = append(refs, b.Ref)
		bo[k] = b
	}
	dt, err := json.Marshal(def)
	if err != nil {
		return err
	}
	if err := saveLocalStateGroup(dockerCli, groupRef, localstate.StateGroup{
		Definition: dt,
		Targets:    targets,
		Inputs:     overrides,
		Refs:       refs,
	}); err != nil {
		return err
	}

	resp, err = build.Build(ctx, nodes, bo, dockerutil.NewClient(dockerCli), confutil.ConfigDir(dockerCli), printer)
	if err != nil {
		return wrapBuildError(err, true)
	}

	err = printer.Wait()
	printer = nil
	if err != nil {
		return err
	}

	var callFormatJSON bool
	var jsonResults = map[string]map[string]any{}
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
		if req.PrintFunc == nil {
			continue
		}

		pf := &pb.PrintFunc{
			Name:         req.PrintFunc.Name,
			Format:       req.PrintFunc.Format,
			IgnoreStatus: req.PrintFunc.IgnoreStatus,
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
			if code, err := printResult(buf, pf, res); err != nil {
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
			if code, err := printResult(dockerCli.Out(), pf, res); err != nil {
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
				printName := bo[name].PrintFunc.Name
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
	flags.BoolVar(&options.printOnly, "print", false, "Print the options without building")
	flags.BoolVar(&options.listTargets, "list-targets", false, "List available targets")
	flags.BoolVar(&options.listVars, "list-variables", false, "List defined variables")
	flags.BoolVar(&options.exportPush, "push", false, `Shorthand for "--set=*.output=type=registry"`)
	flags.StringVar(&options.sbom, "sbom", "", `Shorthand for "--set=*.attest=type=sbom"`)
	flags.StringVar(&options.provenance, "provenance", "", `Shorthand for "--set=*.attest=type=provenance"`)
	flags.StringArrayVar(&options.overrides, "set", nil, `Override target value (e.g., "targetpattern.key=value")`)
	flags.StringVar(&options.callFunc, "call", "build", `Set method for evaluating build ("check", "outline", "targets")`)
	flags.VarPF(callAlias(&options.callFunc, "check"), "check", "", `Shorthand for "--call=check"`)
	flags.Lookup("check").NoOptDefVal = "true"

	commonBuildFlags(&cFlags, flags)

	return cmd
}

func saveLocalStateGroup(dockerCli command.Cli, ref string, lsg localstate.StateGroup) error {
	l, err := localstate.New(confutil.ConfigDir(dockerCli))
	if err != nil {
		return err
	}
	return l.SaveGroup(ref, lsg)
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

func printVars(w io.Writer, vars []*hclparser.Variable) error {
	slices.SortFunc(vars, func(a, b *hclparser.Variable) int {
		return cmp.Compare(a.Name, b.Name)
	})
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

func printTargetList(w io.Writer, cfg *bake.Config) error {
	tw := tabwriter.NewWriter(w, 1, 8, 1, '\t', 0)
	defer tw.Flush()

	tw.Write([]byte("TARGET\tDESCRIPTION\n"))

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

	for _, tgt := range list {
		if strings.HasPrefix(tgt.name, "_") {
			// convention for a private target
			continue
		}
		var descr string
		if tgt.target != nil {
			descr = tgt.target.Description
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
		}
		fmt.Fprintf(tw, "%s\t%s\n", tgt.name, descr)
	}

	return nil
}
