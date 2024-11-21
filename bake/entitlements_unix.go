//go:build !windows
// +build !windows

package bake

import (
	"path/filepath"

	"github.com/pkg/errors"
)

func evaluatePaths(in []string) ([]string, error) {
	out := make([]string, 0, len(in))
	for _, p := range in {
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
