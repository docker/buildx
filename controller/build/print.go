package build

import (
	"bytes"
	"fmt"
	"io"
	"log"

	"github.com/docker/buildx/build"
	"github.com/docker/buildx/util/progress"
	"github.com/docker/docker/api/types/versions"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/frontend/subrequests"
	"github.com/moby/buildkit/frontend/subrequests/outline"
	"github.com/moby/buildkit/frontend/subrequests/targets"
	"github.com/moby/buildkit/solver/errdefs"
	"github.com/morikuni/aec"
	"github.com/sirupsen/logrus"
)

// PrintResult writes the result information to the specified writer.
func PrintResult(w io.Writer, f *build.PrintFunc, res map[string]string) error {
	switch f.Name {
	case "outline":
		return printValue(outline.PrintOutline, outline.SubrequestsOutlineDefinition.Version, f.Format, res, w)
	case "targets":
		return printValue(targets.PrintTargets, targets.SubrequestsTargetsDefinition.Version, f.Format, res, w)
	case "subrequests.describe":
		return printValue(subrequests.PrintDescribe, subrequests.SubrequestsDescribeDefinition.Version, f.Format, res, w)
	default:
		if dt, ok := res["result.txt"]; ok {
			fmt.Fprint(w, dt)
		} else {
			log.Printf("%s %+v", f, res)
		}
	}
	return nil
}

type printFunc func([]byte, io.Writer) error

func printValue(printer printFunc, version string, format string, res map[string]string, w io.Writer) error {
	if format == "json" {
		fmt.Fprintln(w, res["result.json"])
		return nil
	}

	if res["version"] != "" && versions.LessThan(version, res["version"]) && res["result.txt"] != "" {
		// structure is too new and we don't know how to print it
		fmt.Fprint(w, res["result.txt"])
		return nil
	}
	return printer([]byte(res["result.json"]), w)
}

// PrintWarnings writes the warning information to the specified writer.
func PrintWarnings(w io.Writer, warnings []client.VertexWarning, mode string) {
	if len(warnings) == 0 || mode == progress.PrinterModeQuiet {
		return
	}
	fmt.Fprintf(w, "\n ")
	sb := &bytes.Buffer{}
	if len(warnings) == 1 {
		fmt.Fprintf(sb, "1 warning found")
	} else {
		fmt.Fprintf(sb, "%d warnings found", len(warnings))
	}
	if logrus.GetLevel() < logrus.DebugLevel {
		fmt.Fprintf(sb, " (use --debug to expand)")
	}
	fmt.Fprintf(sb, ":\n")
	fmt.Fprint(w, aec.Apply(sb.String(), aec.YellowF))

	for _, warn := range warnings {
		fmt.Fprintf(w, " - %s\n", warn.Short)
		if logrus.GetLevel() < logrus.DebugLevel {
			continue
		}
		for _, d := range warn.Detail {
			fmt.Fprintf(w, "%s\n", d)
		}
		if warn.URL != "" {
			fmt.Fprintf(w, "More info: %s\n", warn.URL)
		}
		if warn.SourceInfo != nil && warn.Range != nil {
			src := errdefs.Source{
				Info:   warn.SourceInfo,
				Ranges: warn.Range,
			}
			src.Print(w)
		}
		fmt.Fprintf(w, "\n")

	}
}
