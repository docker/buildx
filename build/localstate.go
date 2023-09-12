package build

import (
	"path/filepath"

	"github.com/docker/buildx/builder"
	"github.com/docker/buildx/localstate"
	"github.com/moby/buildkit/client"
)

func saveLocalState(so client.SolveOpt, opt Options, node builder.Node, configDir string) error {
	var err error

	if so.Ref == "" {
		return nil
	}

	lp := opt.Inputs.ContextPath
	dp := opt.Inputs.DockerfilePath
	if lp != "" || dp != "" {
		if lp != "" {
			lp, err = filepath.Abs(lp)
			if err != nil {
				return err
			}
		}
		if dp != "" {
			dp, err = filepath.Abs(dp)
			if err != nil {
				return err
			}
		}
		ls, err := localstate.New(configDir)
		if err != nil {
			return err
		}
		if err := ls.SaveRef(node.Builder, node.Name, so.Ref, localstate.State{
			LocalPath:      lp,
			DockerfilePath: dp,
		}); err != nil {
			return err
		}
	}

	return nil
}
