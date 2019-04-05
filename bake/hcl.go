package bake

import "github.com/hashicorp/hcl"

func ParseHCL(dt []byte) (*Config, error) {
	var c Config
	if err := hcl.Unmarshal(dt, &c); err != nil {
		return nil, err
	}
	return &c, nil
}
