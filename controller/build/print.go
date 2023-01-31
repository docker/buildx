package build

import (
	"fmt"
	"io"
	"log"
	"os"

	"github.com/docker/buildx/build"
	"github.com/docker/docker/api/types/versions"
	"github.com/moby/buildkit/frontend/subrequests"
	"github.com/moby/buildkit/frontend/subrequests/outline"
	"github.com/moby/buildkit/frontend/subrequests/targets"
)

func printResult(f *build.PrintFunc, res map[string]string) error {
	switch f.Name {
	case "outline":
		return printValue(outline.PrintOutline, outline.SubrequestsOutlineDefinition.Version, f.Format, res)
	case "targets":
		return printValue(targets.PrintTargets, targets.SubrequestsTargetsDefinition.Version, f.Format, res)
	case "subrequests.describe":
		return printValue(subrequests.PrintDescribe, subrequests.SubrequestsDescribeDefinition.Version, f.Format, res)
	default:
		if dt, ok := res["result.txt"]; ok {
			fmt.Print(dt)
		} else {
			log.Printf("%s %+v", f, res)
		}
	}
	return nil
}

type printFunc func([]byte, io.Writer) error

func printValue(printer printFunc, version string, format string, res map[string]string) error {
	if format == "json" {
		fmt.Fprintln(os.Stdout, res["result.json"])
		return nil
	}

	if res["version"] != "" && versions.LessThan(version, res["version"]) && res["result.txt"] != "" {
		// structure is too new and we don't know how to print it
		fmt.Fprint(os.Stdout, res["result.txt"])
		return nil
	}
	return printer([]byte(res["result.json"]), os.Stdout)
}
