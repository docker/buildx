package bake

import (
	"os"
	"strings"

	hcl "github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/ext/userfunc"
	"github.com/hashicorp/hcl/v2/hclsimple"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hashicorp/hcl/v2/json"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/function"
	"github.com/zclconf/go-cty/cty/function/stdlib"
)

// Collection of generally useful functions in cty-using applications, which
// HCL supports. These functions are available for use in HCL files.
var (
	stdlibFunctions = map[string]function.Function{
		"absolute":               stdlib.AbsoluteFunc,
		"add":                    stdlib.AddFunc,
		"and":                    stdlib.AndFunc,
		"byteslen":               stdlib.BytesLenFunc,
		"bytesslice":             stdlib.BytesSliceFunc,
		"csvdecode":              stdlib.CSVDecodeFunc,
		"coalesce":               stdlib.CoalesceFunc,
		"concat":                 stdlib.ConcatFunc,
		"divide":                 stdlib.DivideFunc,
		"equal":                  stdlib.EqualFunc,
		"formatdate":             stdlib.FormatDateFunc,
		"format":                 stdlib.FormatFunc,
		"formatlist":             stdlib.FormatListFunc,
		"greaterthan":            stdlib.GreaterThanFunc,
		"greaterthanorequalto":   stdlib.GreaterThanOrEqualToFunc,
		"hasindex":               stdlib.HasIndexFunc,
		"index":                  stdlib.IndexFunc,
		"int":                    stdlib.IntFunc,
		"jsondecode":             stdlib.JSONDecodeFunc,
		"jsonencode":             stdlib.JSONEncodeFunc,
		"length":                 stdlib.LengthFunc,
		"lessthan":               stdlib.LessThanFunc,
		"lessthanorequalto":      stdlib.LessThanOrEqualToFunc,
		"lower":                  stdlib.LowerFunc,
		"max":                    stdlib.MaxFunc,
		"min":                    stdlib.MinFunc,
		"modulo":                 stdlib.ModuloFunc,
		"multiply":               stdlib.MultiplyFunc,
		"negate":                 stdlib.NegateFunc,
		"notequal":               stdlib.NotEqualFunc,
		"not":                    stdlib.NotFunc,
		"or":                     stdlib.OrFunc,
		"range":                  stdlib.RangeFunc,
		"regexall":               stdlib.RegexAllFunc,
		"regex":                  stdlib.RegexFunc,
		"reverse":                stdlib.ReverseFunc,
		"sethaselement":          stdlib.SetHasElementFunc,
		"setintersection":        stdlib.SetIntersectionFunc,
		"setsubtract":            stdlib.SetSubtractFunc,
		"setsymmetricdifference": stdlib.SetSymmetricDifferenceFunc,
		"setunion":               stdlib.SetUnionFunc,
		"strlen":                 stdlib.StrlenFunc,
		"substr":                 stdlib.SubstrFunc,
		"subtract":               stdlib.SubtractFunc,
		"upper":                  stdlib.UpperFunc,
	}
)

// Used in the first pass of decoding instead of the Config struct to disallow
// interpolation while parsing variable blocks.
type staticConfig struct {
	Variables []*Variable `hcl:"variable,block"`
	Remain    hcl.Body    `hcl:",remain"`
}

func ParseHCL(dt []byte, fn string) (*Config, error) {
	// Decode user defined functions, first parsing as hcl and falling back to
	// json, returning errors based on the file suffix.
	file, hcldiags := hclsyntax.ParseConfig(dt, fn, hcl.Pos{Line: 1, Column: 1})
	if hcldiags.HasErrors() {
		var jsondiags hcl.Diagnostics
		file, jsondiags = json.Parse(dt, fn)
		if jsondiags.HasErrors() {
			fnl := strings.ToLower(fn)
			if strings.HasSuffix(fnl, ".json") {
				return nil, jsondiags
			} else {
				return nil, hcldiags
			}
		}
	}

	userFunctions, _, diags := userfunc.DecodeUserFunctions(file.Body, "function", func() *hcl.EvalContext {
		return &hcl.EvalContext{
			Functions: stdlibFunctions,
		}
	})
	if diags.HasErrors() {
		return nil, diags
	}

	var sc staticConfig

	// Decode only variable blocks without interpolation.
	if err := hclsimple.Decode(fn, dt, nil, &sc); err != nil {
		return nil, err
	}

	// Set all variables to their default value if defined.
	variables := make(map[string]cty.Value)
	for _, variable := range sc.Variables {
		variables[variable.Name] = cty.StringVal(variable.Default)
	}

	// Override default with values from environment.
	for _, env := range os.Environ() {
		parts := strings.SplitN(env, "=", 2)
		name, value := parts[0], parts[1]
		if _, ok := variables[name]; ok {
			variables[name] = cty.StringVal(value)
		}
	}

	functions := make(map[string]function.Function)
	for k, v := range stdlibFunctions {
		functions[k] = v
	}
	for k, v := range userFunctions {
		functions[k] = v
	}

	ctx := &hcl.EvalContext{
		Variables: variables,
		Functions: functions,
	}

	var c Config

	// Decode with variables and functions.
	if err := hclsimple.Decode(fn, dt, ctx, &c); err != nil {
		return nil, err
	}
	return &c, nil
}
