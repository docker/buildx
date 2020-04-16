package bake

import (
	"os"
	"strings"

	hcl "github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsimple"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/function"
	"github.com/zclconf/go-cty/cty/function/stdlib"
)

// Collection of functions expected to be generally useful in cty-using
// applications, which HCL supports. This set of functions will be available to
// be called in HCL files.
var (
	functions = map[string]function.Function{
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

func ParseHCL(dt []byte, fn string) (*Config, error) {
	variables := make(map[string]cty.Value)
	for _, env := range os.Environ() {
		parts := strings.SplitN(env, "=", 2)
		variables[parts[0]] = cty.StringVal(parts[1])
	}

	ctx := &hcl.EvalContext{
		Variables: variables,
		Functions: functions,
	}

	var c Config
	if err := hclsimple.Decode(fn, dt, ctx, &c); err != nil {
		return nil, err
	}
	return &c, nil
}
