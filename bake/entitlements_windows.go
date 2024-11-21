package bake

import (
	"os"
	"path/filepath"

	"github.com/pkg/errors"
)

func evaluatePaths(in []string) ([]string, error) {
	out := make([]string, 0, len(in))
	for _, p := range in {
		if p == "/" {
			out = append(out, getAllVolumes()...)
			continue
		}
		v, err := filepath.Abs(p)
		if err != nil {
			return nil, err
		}
		v, err = filepath.EvalSymlinks(v)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to evaluate path %q", p)
		}
		out = append(out, v)
	}
	return out, nil
}

func getAllVolumes() []string {
	var volumes []string
	for _, drive := range "ABCDEFGHIJKLMNOPQRSTUVWXYZ" {
		p := string(drive) + ":" + string(filepath.Separator)
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			volumes = append(volumes, p)
		}
	}
	return volumes
}
