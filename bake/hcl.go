package bake

import "github.com/hashicorp/hcl/v2/hclsimple"

func ParseHCL(dt []byte, fn string) (*Config, error) {
	var c Config
	if err := hclsimple.Decode(fn, dt, nil, &c); err != nil {
		return nil, err
	}
	return &c, nil
}
