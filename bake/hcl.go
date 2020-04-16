package bake

import (
	"os"
	"strings"

	hcl "github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsimple"
	"github.com/zclconf/go-cty/cty"
)

func ParseHCL(dt []byte, fn string) (*Config, error) {
	variables := make(map[string]cty.Value)
	for _, env := range os.Environ() {
		parts := strings.SplitN(env, "=", 2)
		variables[parts[0]] = cty.StringVal(parts[1])
	}

	ctx := &hcl.EvalContext{
		Variables: variables,
	}

	var c Config
	if err := hclsimple.Decode(fn, dt, ctx, &c); err != nil {
		return nil, err
	}
	return &c, nil
}
