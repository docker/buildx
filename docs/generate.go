package main

import (
	"fmt"
	"log"
	"os"
	"regexp"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/docker/buildx/bake/hclparser"
	"github.com/docker/buildx/commands"
	clidocstool "github.com/docker/cli-docs-tool"
	"github.com/docker/cli/cli/command"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	// import drivers otherwise factories are empty
	// for --driver output flag usage
	_ "github.com/docker/buildx/driver/docker"
	_ "github.com/docker/buildx/driver/docker-container"
	_ "github.com/docker/buildx/driver/kubernetes"
	_ "github.com/docker/buildx/driver/remote"
)

const defaultSourcePath = "docs/reference/"
const defaultBakeStdlibSourcePath = "docs/bake-stdlib.md"

var adjustSep = regexp.MustCompile(`\|:---(\s+)`)

type options struct {
	source     string
	bakeSource string
	formats    []string
}

// fixUpExperimentalCLI trims the " (EXPERIMENTAL)" suffix from the CLI output,
// as docs.docker.com already displays "experimental (CLI)",
//
// https://github.com/docker/buildx/pull/2188#issuecomment-1889487022
func fixUpExperimentalCLI(cmd *cobra.Command) {
	const (
		annotationExperimentalCLI = "experimentalCLI"
		suffixExperimental        = " (EXPERIMENTAL)"
	)
	if _, ok := cmd.Annotations[annotationExperimentalCLI]; ok {
		cmd.Short = strings.TrimSuffix(cmd.Short, suffixExperimental)
	}
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if _, ok := f.Annotations[annotationExperimentalCLI]; ok {
			f.Usage = strings.TrimSuffix(f.Usage, suffixExperimental)
		}
	})
	for _, c := range cmd.Commands() {
		fixUpExperimentalCLI(c)
	}
}

func gen(opts *options) error {
	log.SetFlags(0)

	dockerCLI, err := command.NewDockerCli()
	if err != nil {
		return err
	}
	cmd := &cobra.Command{
		Use:               "docker [OPTIONS] COMMAND [ARG...]",
		Short:             "The base command for the Docker CLI.",
		DisableAutoGenTag: true,
	}

	cmd.AddCommand(commands.NewRootCmd("buildx", true, dockerCLI))

	c, err := clidocstool.New(clidocstool.Options{
		Root:      cmd,
		SourceDir: opts.source,
		Plugin:    true,
	})
	if err != nil {
		return err
	}

	for _, format := range opts.formats {
		switch format {
		case "md":
			if err = c.GenMarkdownTree(cmd); err != nil {
				return err
			}
			if err = generateBakeStdlibDocs(opts.bakeSource); err != nil {
				return errors.Wrap(err, "generating bake stdlib docs")
			}
		case "yaml":
			// fix up is needed only for yaml (used for generating docs.docker.com contents)
			fixUpExperimentalCLI(cmd)
			if err = c.GenYamlTree(cmd); err != nil {
				return err
			}
		default:
			return errors.Errorf("unknown format %q", format)
		}
	}

	return nil
}

func run() error {
	opts := &options{}
	flags := pflag.NewFlagSet(os.Args[0], pflag.ContinueOnError)
	flags.StringVar(&opts.source, "source", defaultSourcePath, "Docs source folder")
	flags.StringVar(&opts.bakeSource, "bake-stdlib-source", defaultBakeStdlibSourcePath, "Bake stdlib source file")
	flags.StringSliceVar(&opts.formats, "formats", []string{}, "Format (md, yaml)")
	if err := flags.Parse(os.Args[1:]); err != nil {
		return err
	}
	if len(opts.formats) == 0 {
		return errors.New("Docs format required")
	}
	return gen(opts)
}

func main() {
	if err := run(); err != nil {
		log.Printf("ERROR: %+v", err)
		os.Exit(1)
	}
}

func generateBakeStdlibDocs(filename string) error {
	log.Printf("INFO: Generating Markdown for %q", filename)
	dt, err := os.ReadFile(filename)
	if err != nil {
		return err
	}
	currentContent := string(dt)

	start := strings.Index(currentContent, "<!---MARKER_STDLIB_START-->")
	end := strings.Index(currentContent, "<!---MARKER_STDLIB_END-->")
	if start == -1 {
		return errors.Errorf("no start marker in %s", filename)
	}
	if end == -1 {
		return errors.Errorf("no end marker in %s", filename)
	}

	table := newMdTable("Name", "Description")
	names := make([]string, 0, len(hclparser.Stdlib()))
	for name := range hclparser.Stdlib() {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fname := fmt.Sprintf("`%s`", name)
		if strings.Contains(currentContent, "<a name=\""+name+"\"></a>") {
			fname = fmt.Sprintf("[`%s`](#%s)", name, name)
		}
		fdesc := hclparser.StdlibFuncDescription(name)
		if fdesc == "" {
			return errors.Errorf("function %q has no description", name)
		}
		table.AddRow(fname, fdesc)
	}

	newContent := currentContent[:start] + "<!---MARKER_STDLIB_START-->\n\n" + table.String() + "\n" + currentContent[end:]
	return os.WriteFile(filename, []byte(newContent), 0644)
}

type mdTable struct {
	out       *strings.Builder
	tabWriter *tabwriter.Writer
}

func newMdTable(headers ...string) *mdTable {
	w := &strings.Builder{}
	t := &mdTable{
		out:       w,
		tabWriter: tabwriter.NewWriter(w, 5, 5, 1, ' ', tabwriter.Debug),
	}
	t.addHeader(headers...)
	return t
}

func (t *mdTable) addHeader(cols ...string) {
	t.AddRow(cols...)
	_, _ = t.tabWriter.Write([]byte("|" + strings.Repeat(":---\t", len(cols)) + "\n"))
}

func (t *mdTable) AddRow(cols ...string) {
	for i := range cols {
		cols[i] = mdEscapePipe(cols[i])
	}
	_, _ = t.tabWriter.Write([]byte("| " + strings.Join(cols, "\t ") + "\t\n"))
}

func (t *mdTable) String() string {
	_ = t.tabWriter.Flush()
	return adjustSep.ReplaceAllStringFunc(t.out.String()+"\n", func(in string) string {
		return strings.ReplaceAll(in, " ", "-")
	})
}

func mdEscapePipe(s string) string {
	return strings.ReplaceAll(s, `|`, `\|`)
}
