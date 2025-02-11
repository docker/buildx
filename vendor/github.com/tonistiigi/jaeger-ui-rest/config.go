package jaegerui

import (
	"bytes"
	"encoding/json"
	"slices"
)

type Menu struct {
	Label string     `json:"label"`
	Items []MenuItem `json:"items"`
}

type MenuItem struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

type Config struct {
	Dependencies struct {
		MenuEnabled bool `json:"menuEnabled"`
	} `json:"dependencies"`
	Monitor struct {
		MenuEnabled bool `json:"menuEnabled"`
	} `json:"monitor"`
	ArchiveEnabled bool   `json:"archiveEnabled"`
	Menu           []Menu `json:"menu"`
}

func (cfg Config) Inject(name string, dt []byte) ([]byte, bool) {
	if name != "index.html" {
		return dt, false
	}

	cfgData, err := json.Marshal(cfg)
	if err != nil {
		return dt, false
	}

	dt = bytes.Replace(dt, []byte("const JAEGER_CONFIG = DEFAULT_CONFIG;"), slices.Concat([]byte(`const JAEGER_CONFIG = `), cfgData, []byte(`;`)), 1)
	return dt, true
}
