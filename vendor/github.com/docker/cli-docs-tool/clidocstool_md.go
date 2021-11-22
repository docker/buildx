// Copyright 2021 cli-docs-tool authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package clidocstool

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/docker/cli-docs-tool/annotation"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// GenMarkdownTree will generate a markdown page for this command and all
// descendants in the directory given.
func (c *Client) GenMarkdownTree(cmd *cobra.Command) error {
	for _, sc := range cmd.Commands() {
		if err := c.GenMarkdownTree(sc); err != nil {
			return err
		}
	}

	// always disable the addition of [flags] to the usage
	cmd.DisableFlagsInUseLine = true

	// Skip the root command altogether, to prevent generating a useless
	// md file for plugins.
	if c.plugin && !cmd.HasParent() {
		return nil
	}

	log.Printf("INFO: Generating Markdown for %q", cmd.CommandPath())
	mdFile := mdFilename(cmd)
	sourcePath := filepath.Join(c.source, mdFile)
	targetPath := filepath.Join(c.target, mdFile)

	// check recursively to handle inherited annotations
	for curr := cmd; curr != nil; curr = curr.Parent() {
		if _, ok := cmd.Annotations[annotation.CodeDelimiter]; !ok {
			if cd, cok := curr.Annotations[annotation.CodeDelimiter]; cok {
				if cmd.Annotations == nil {
					cmd.Annotations = map[string]string{}
				}
				cmd.Annotations[annotation.CodeDelimiter] = cd
			}
		}
	}

	if !fileExists(sourcePath) {
		var icBuf bytes.Buffer
		icTpl, err := template.New("ic").Option("missingkey=error").Parse(`# {{ .Command }}

<!---MARKER_GEN_START-->
<!---MARKER_GEN_END-->

`)
		if err != nil {
			return err
		}
		if err = icTpl.Execute(&icBuf, struct {
			Command string
		}{
			Command: cmd.CommandPath(),
		}); err != nil {
			return err
		}
		if err = ioutil.WriteFile(targetPath, icBuf.Bytes(), 0644); err != nil {
			return err
		}
	} else if err := copyFile(sourcePath, targetPath); err != nil {
		return err
	}

	content, err := ioutil.ReadFile(targetPath)
	if err != nil {
		return err
	}

	cs := string(content)

	start := strings.Index(cs, "<!---MARKER_GEN_START-->")
	end := strings.Index(cs, "<!---MARKER_GEN_END-->")

	if start == -1 {
		return fmt.Errorf("no start marker in %s", mdFile)
	}
	if end == -1 {
		return fmt.Errorf("no end marker in %s", mdFile)
	}

	out, err := mdCmdOutput(cmd, cs)
	if err != nil {
		return err
	}
	cont := cs[:start] + "<!---MARKER_GEN_START-->" + "\n" + out + "\n" + cs[end:]

	fi, err := os.Stat(targetPath)
	if err != nil {
		return err
	}
	if err = ioutil.WriteFile(targetPath, []byte(cont), fi.Mode()); err != nil {
		return fmt.Errorf("failed to write %s: %w", targetPath, err)
	}

	return nil
}

func mdFilename(cmd *cobra.Command) string {
	name := cmd.CommandPath()
	if i := strings.Index(name, " "); i >= 0 {
		name = name[i+1:]
	}
	return strings.ReplaceAll(name, " ", "_") + ".md"
}

func mdMakeLink(txt, link string, f *pflag.Flag, isAnchor bool) string {
	link = "#" + link
	annotations, ok := f.Annotations[annotation.ExternalURL]
	if ok && len(annotations) > 0 {
		link = annotations[0]
	} else {
		if !isAnchor {
			return txt
		}
	}

	return "[" + txt + "](" + link + ")"
}

func mdCmdOutput(cmd *cobra.Command, old string) (string, error) {
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
			fmt.Fprintf(b, "| [`%s`](%s) | %s |\n", c.Name(), mdFilename(c), c.Short)
		}
		fmt.Fprint(b, "\n\n")
	}

	// add inherited flags before checking for flags availability
	cmd.Flags().AddFlagSet(cmd.InheritedFlags())

	if cmd.Flags().HasAvailableFlags() {
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
				name = mdMakeLink(name, f.Name, f, isLink)
				fmt.Fprintf(b, "%s, ", name)
			}
			name := "`--" + f.Name
			if f.Value.Type() != "bool" {
				name += " " + f.Value.Type()
			}
			name += "`"
			name = mdMakeLink(name, f.Name, f, isLink)
			usage := f.Usage
			if cd, ok := f.Annotations[annotation.CodeDelimiter]; ok {
				usage = strings.ReplaceAll(usage, cd[0], "`")
			} else if cd, ok := cmd.Annotations[annotation.CodeDelimiter]; ok {
				usage = strings.ReplaceAll(usage, cd, "`")
			}
			fmt.Fprintf(b, "%s | %s |\n", mdEscapePipe(name), mdEscapePipe(usage))
		})
		fmt.Fprintln(b, "")
	}

	return b.String(), nil
}

func mdEscapePipe(s string) string {
	return strings.ReplaceAll(s, `|`, `\|`)
}
