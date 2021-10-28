package confutil

import (
	"os"

	"github.com/pelletier/go-toml"
	"github.com/pkg/errors"
)

// loadConfigTree loads BuildKit config toml tree
func loadConfigTree(fp string) (*toml.Tree, error) {
	f, err := os.Open(fp)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, errors.Wrapf(err, "failed to load config from %s", fp)
	}
	defer f.Close()
	t, err := toml.LoadReader(f)
	if err != nil {
		return t, errors.Wrap(err, "failed to parse config")
	}
	return t, nil
}
