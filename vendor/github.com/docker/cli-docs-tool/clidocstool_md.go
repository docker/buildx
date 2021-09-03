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

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// GenMarkdownTree will generate a markdown page for this command and all
// descendants in the directory given.
func GenMarkdownTree(cmd *cobra.Command, dir string) error {
	for _, c := range cmd.Commands() {
		if err := GenMarkdownTree(c, dir); err != nil {
			return err
		}
	}
	if !cmd.HasParent() {
		return nil
	}

	log.Printf("INFO: Generating Markdown for %q", cmd.CommandPath())
	mdFile := mdFilename(cmd)
	fullPath := filepath.Join(dir, mdFile)

	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
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
		if err = ioutil.WriteFile(fullPath, icBuf.Bytes(), 0644); err != nil {
			return err
		}
	}

	content, err := ioutil.ReadFile(fullPath)
	if err != nil {
		return err
	}

	cs := string(content)

	start := strings.Index(cs, "<!---MARKER_GEN_START-->")
	end := strings.Index(cs, "<!---MARKER_GEN_END-->")

	if start == -1 {
		return errors.Errorf("no start marker in %s", mdFile)
	}
	if end == -1 {
		return errors.Errorf("no end marker in %s", mdFile)
	}

	out, err := mdCmdOutput(cmd, cs)
	if err != nil {
		return err
	}
	cont := cs[:start] + "<!---MARKER_GEN_START-->" + "\n" + out + "\n" + cs[end:]

	fi, err := os.Stat(fullPath)
	if err != nil {
		return err
	}
	if err := ioutil.WriteFile(fullPath, []byte(cont), fi.Mode()); err != nil {
		return errors.Wrapf(err, "failed to write %s", fullPath)
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
				name = mdMakeLink(name, f.Name, f, isLink)
				fmt.Fprintf(b, "%s, ", name)
			}
			name := "`--" + f.Name
			if f.Value.Type() != "bool" {
				name += " " + f.Value.Type()
			}
			name += "`"
			name = mdMakeLink(name, f.Name, f, isLink)
			fmt.Fprintf(b, "%s | %s |\n", mdEscapePipe(name), mdEscapePipe(f.Usage))
		})
		fmt.Fprintln(b, "")
	}

	return b.String(), nil
}

func mdEscapePipe(s string) string {
	return strings.ReplaceAll(s, `|`, `\|`)
}
