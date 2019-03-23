package farm

import (
	"os"
	"path/filepath"
)

// Name of a Farm.
type Name string

// Context name of docker cli context.
type Context string

// Platform represents the platform OS/Architecture of a Context.
type Platform string

// Farm is a set of Context objects mapped to a Platform.
// This could change in the future to be more generic.
type Farm map[Platform]Context

// Ville is a set of Farms.
type Ville struct {
	Farms map[Name]Farm
	ActiveFarm Name
}

func LoadVille(root string) (*Ville, error) {
	if err := os.MkdirAll(root, 0755); err != nil {
		return err
	}
	v := &Ville{Farms: make(map[Name]Farm)}
	err := filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if p == root {
			return nil
		}
		if fi.IsDir() {
			return filepath.SkipDir
		}
		v.Farms[fi.Name()] = map[Platform]Context{}
	})
	return v, err
}
