package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/docker/buildx/commands"
	"github.com/docker/cli/cli/command"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const descriptionSourcePath = "docs/reference/"

func generateDocs(opts *options) error {
	dockerCLI, err := command.NewDockerCli()
	if err != nil {
		return err
	}
	cmd := &cobra.Command{
		Use:   "docker [OPTIONS] COMMAND [ARG...]",
		Short: "The base command for the Docker CLI.",
	}
	cmd.AddCommand(commands.NewRootCmd("buildx", true, dockerCLI))
	return genCmd(cmd, opts.target)
}

func getMDFilename(cmd *cobra.Command) string {
	name := cmd.CommandPath()
	if i := strings.Index(name, " "); i >= 0 {
		name = name[i+1:]
	}
	return strings.ReplaceAll(name, " ", "_") + ".md"
}

func genCmd(cmd *cobra.Command, dir string) error {
	for _, c := range cmd.Commands() {
		if err := genCmd(c, dir); err != nil {
			return err
		}
	}
	if !cmd.HasParent() {
		return nil
	}

	mdFile := getMDFilename(cmd)
	fullPath := filepath.Join(dir, mdFile)

	content, err := ioutil.ReadFile(fullPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errors.Wrapf(err, "%s does not exist", mdFile)
		}
	}

	cs := string(content)

	markerStart := "<!---MARKER_GEN_START-->"
	markerEnd := "<!---MARKER_GEN_END-->"

	start := strings.Index(cs, markerStart)
	end := strings.Index(cs, markerEnd)

	if start == -1 {
		return errors.Errorf("no start marker in %s", mdFile)
	}
	if end == -1 {
		return errors.Errorf("no end marker in %s", mdFile)
	}

	out, err := cmdOutput(cmd, cs)
	if err != nil {
		return err
	}
	cont := cs[:start] + markerStart + "\n" + out + "\n" + cs[end:]

	fi, err := os.Stat(fullPath)
	if err != nil {
		return err
	}
	if err := ioutil.WriteFile(fullPath, []byte(cont), fi.Mode()); err != nil {
		return errors.Wrapf(err, "failed to write %s", fullPath)
	}
	log.Printf("updated %s", fullPath)
	return nil
}

func makeLink(txt, link string, f *pflag.Flag, isAnchor bool) string {
	link = "#" + link
	annotations, ok := f.Annotations["docs.external.url"]
	if ok && len(annotations) > 0 {
		link = annotations[0]
	} else {
		if !isAnchor {
			return txt
		}
	}

	return "[" + txt + "](" + link + ")"
}

func cmdOutput(cmd *cobra.Command, old string) (string, error) {
	b := &strings.Builder{}

	desc := cmd.Short
	if cmd.Long != "" {
		desc = cmd.Long
	}
	if desc != "" {
		fmt.Fprintf(b, "%s\n\n", desc)
	}

	if len(cmd.Aliases) != 0 {
		fmt.Fprintf(b, "### Aliases\n\n`%s`", cmd.Name())
		for _, a := range cmd.Aliases {
			fmt.Fprintf(b, ", `%s`", a)
		}
		fmt.Fprint(b, "\n\n")
	}

	if len(cmd.Commands()) != 0 {
		fmt.Fprint(b, "### Subcommands\n\n")
		fmt.Fprint(b, "| Name | Description |\n")
		fmt.Fprint(b, "| --- | --- |\n")
		for _, c := range cmd.Commands() {
			fmt.Fprintf(b, "| [`%s`](%s) | %s |\n", c.Name(), getMDFilename(c), c.Short)
		}
		fmt.Fprint(b, "\n\n")
	}

	hasFlags := cmd.Flags().HasAvailableFlags()

	cmd.Flags().AddFlagSet(cmd.InheritedFlags())

	if hasFlags {
		fmt.Fprint(b, "### Options\n\n")
		fmt.Fprint(b, "| Name | Description |\n")
		fmt.Fprint(b, "| --- | --- |\n")

		cmd.Flags().VisitAll(func(f *pflag.Flag) {
			if f.Hidden {
				return
			}
			isLink := strings.Contains(old, "<a name=\""+f.Name+"\"></a>")
			fmt.Fprint(b, "| ")
			if f.Shorthand != "" {
				name := "`-" + f.Shorthand + "`"
				name = makeLink(name, f.Name, f, isLink)
				fmt.Fprintf(b, "%s, ", name)
			}
			name := "`--" + f.Name
			if f.Value.Type() != "bool" {
				name += " " + f.Value.Type()
			}
			name += "`"
			name = makeLink(name, f.Name, f, isLink)
			fmt.Fprintf(b, "%s | %s |\n", name, f.Usage)
		})
		fmt.Fprintln(b, "")
	}

	return b.String(), nil
}

type options struct {
	target string
}

func parseArgs() (*options, error) {
	opts := &options{}
	flags := pflag.NewFlagSet(os.Args[0], pflag.ContinueOnError)
	flags.StringVar(&opts.target, "target", descriptionSourcePath, "Docs directory")
	err := flags.Parse(os.Args[1:])
	return opts, err
}

func main() {
	if err := run(); err != nil {
		log.Printf("error: %+v", err)
		os.Exit(1)
	}
}

func run() error {
	opts, err := parseArgs()
	if err != nil {
		return err
	}
	if err := generateDocs(opts); err != nil {
		return err
	}
	return nil
}
