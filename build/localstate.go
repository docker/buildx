package build

import (
	"path/filepath"

	"github.com/docker/buildx/builder"
	"github.com/docker/buildx/localstate"
	"github.com/moby/buildkit/client"
)

func saveLocalState(so *client.SolveOpt, target string, opts Options, node builder.Node, configDir string) error {
	var err error
	if so.Ref == "" {
		return nil
	}
	lp := opts.Inputs.ContextPath
	dp := opts.Inputs.DockerfilePath
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
		l, err := localstate.New(configDir)
		if err != nil {
			return err
		}
		return l.SaveRef(node.Builder, node.Name, so.Ref, localstate.State{
			Target:         target,
			LocalPath:      lp,
			DockerfilePath: dp,
			GroupRef:       opts.GroupRef,
		})
	}
	return nil
}
