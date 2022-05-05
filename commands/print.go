package commands

import (
	"fmt"
	"log"
	"os"

	"github.com/moby/buildkit/frontend/subrequests"
	"github.com/moby/buildkit/frontend/subrequests/outline"
	"github.com/moby/buildkit/frontend/subrequests/targets"
)

func printResult(f string, res map[string]string) error {
	switch f {
	case "outline":
		if err := outline.PrintOutline([]byte(res["result.json"]), os.Stdout); err != nil {
			return err
		}
	case "targets":
		if err := targets.PrintTargets([]byte(res["result.json"]), os.Stdout); err != nil {
			return err
		}
	case "subrequests.describe":
		if err := subrequests.PrintDescribe([]byte(res["result.json"]), os.Stdout); err != nil {
			return err
		}
	default:
		if dt, ok := res["result.txt"]; ok {
			fmt.Print(dt)
		} else {
			log.Printf("%s %+v", f, res)
		}
	}
	return nil
}
