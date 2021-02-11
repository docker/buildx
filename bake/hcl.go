package bake

import (
	"math"
	"math/big"
	"os"
	"strconv"
	"strings"

	"github.com/hashicorp/go-cty-funcs/cidr"
	"github.com/hashicorp/go-cty-funcs/crypto"
	"github.com/hashicorp/go-cty-funcs/encoding"
	"github.com/hashicorp/go-cty-funcs/uuid"
	hcl "github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/ext/tryfunc"
	"github.com/hashicorp/hcl/v2/ext/typeexpr"
	"github.com/hashicorp/hcl/v2/ext/userfunc"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	hcljson "github.com/hashicorp/hcl/v2/json"
	"github.com/moby/buildkit/solver/errdefs"
	"github.com/moby/buildkit/solver/pb"
	"github.com/pkg/errors"
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
		"base64decode":           encoding.Base64DecodeFunc,
		"base64encode":           encoding.Base64EncodeFunc,
		"bcrypt":                 crypto.BcryptFunc,
		"byteslen":               stdlib.BytesLenFunc,
		"bytesslice":             stdlib.BytesSliceFunc,
		"can":                    tryfunc.CanFunc,
		"ceil":                   stdlib.CeilFunc,
		"chomp":                  stdlib.ChompFunc,
		"chunklist":              stdlib.ChunklistFunc,
		"cidrhost":               cidr.HostFunc,
		"cidrnetmask":            cidr.NetmaskFunc,
		"cidrsubnet":             cidr.SubnetFunc,
		"cidrsubnets":            cidr.SubnetsFunc,
		"csvdecode":              stdlib.CSVDecodeFunc,
		"coalesce":               stdlib.CoalesceFunc,
		"coalescelist":           stdlib.CoalesceListFunc,
		"compact":                stdlib.CompactFunc,
		"concat":                 stdlib.ConcatFunc,
		"contains":               stdlib.ContainsFunc,
		"convert":                typeexpr.ConvertFunc,
		"distinct":               stdlib.DistinctFunc,
		"divide":                 stdlib.DivideFunc,
		"element":                stdlib.ElementFunc,
		"equal":                  stdlib.EqualFunc,
		"flatten":                stdlib.FlattenFunc,
		"floor":                  stdlib.FloorFunc,
		"formatdate":             stdlib.FormatDateFunc,
		"format":                 stdlib.FormatFunc,
		"formatlist":             stdlib.FormatListFunc,
		"greaterthan":            stdlib.GreaterThanFunc,
		"greaterthanorequalto":   stdlib.GreaterThanOrEqualToFunc,
		"hasindex":               stdlib.HasIndexFunc,
		"indent":                 stdlib.IndentFunc,
		"index":                  stdlib.IndexFunc,
		"int":                    stdlib.IntFunc,
		"jsondecode":             stdlib.JSONDecodeFunc,
		"jsonencode":             stdlib.JSONEncodeFunc,
		"keys":                   stdlib.KeysFunc,
		"join":                   stdlib.JoinFunc,
		"length":                 stdlib.LengthFunc,
		"lessthan":               stdlib.LessThanFunc,
		"lessthanorequalto":      stdlib.LessThanOrEqualToFunc,
		"log":                    stdlib.LogFunc,
		"lookup":                 stdlib.LookupFunc,
		"lower":                  stdlib.LowerFunc,
		"max":                    stdlib.MaxFunc,
		"md5":                    crypto.Md5Func,
		"merge":                  stdlib.MergeFunc,
		"min":                    stdlib.MinFunc,
		"modulo":                 stdlib.ModuloFunc,
		"multiply":               stdlib.MultiplyFunc,
		"negate":                 stdlib.NegateFunc,
		"notequal":               stdlib.NotEqualFunc,
		"not":                    stdlib.NotFunc,
		"or":                     stdlib.OrFunc,
		"parseint":               stdlib.ParseIntFunc,
		"pow":                    stdlib.PowFunc,
		"range":                  stdlib.RangeFunc,
		"regexall":               stdlib.RegexAllFunc,
		"regex":                  stdlib.RegexFunc,
		"regex_replace":          stdlib.RegexReplaceFunc,
		"reverse":                stdlib.ReverseFunc,
		"reverselist":            stdlib.ReverseListFunc,
		"rsadecrypt":             crypto.RsaDecryptFunc,
		"sethaselement":          stdlib.SetHasElementFunc,
		"setintersection":        stdlib.SetIntersectionFunc,
		"setproduct":             stdlib.SetProductFunc,
		"setsubtract":            stdlib.SetSubtractFunc,
		"setsymmetricdifference": stdlib.SetSymmetricDifferenceFunc,
		"setunion":               stdlib.SetUnionFunc,
		"sha1":                   crypto.Sha1Func,
		"sha256":                 crypto.Sha256Func,
		"sha512":                 crypto.Sha512Func,
		"signum":                 stdlib.SignumFunc,
		"slice":                  stdlib.SliceFunc,
		"sort":                   stdlib.SortFunc,
		"split":                  stdlib.SplitFunc,
		"strlen":                 stdlib.StrlenFunc,
		"substr":                 stdlib.SubstrFunc,
		"subtract":               stdlib.SubtractFunc,
		"timeadd":                stdlib.TimeAddFunc,
		"title":                  stdlib.TitleFunc,
		"trim":                   stdlib.TrimFunc,
		"trimprefix":             stdlib.TrimPrefixFunc,
		"trimspace":              stdlib.TrimSpaceFunc,
		"trimsuffix":             stdlib.TrimSuffixFunc,
		"try":                    tryfunc.TryFunc,
		"upper":                  stdlib.UpperFunc,
		"urlencode":              encoding.URLEncodeFunc,
		"uuidv4":                 uuid.V4Func,
		"uuidv5":                 uuid.V5Func,
		"values":                 stdlib.ValuesFunc,
		"zipmap":                 stdlib.ZipmapFunc,
	}
)

type StaticConfig struct {
	Variables []*Variable `hcl:"variable,block"`
	Remain    hcl.Body    `hcl:",remain"`

	attrs hcl.Attributes

	defaults map[string]*hcl.Attribute
	env      map[string]string
	values   map[string]cty.Value
	progress map[string]struct{}
}

func mergeStaticConfig(scs []*StaticConfig) *StaticConfig {
	if len(scs) == 0 {
		return nil
	}
	sc := scs[0]
	for _, s := range scs[1:] {
		sc.Variables = append(sc.Variables, s.Variables...)
		for k, v := range s.attrs {
			sc.attrs[k] = v
		}
	}
	return sc
}

func (sc *StaticConfig) Values(withEnv bool) (map[string]cty.Value, error) {
	sc.defaults = map[string]*hcl.Attribute{}
	for _, v := range sc.Variables {
		sc.defaults[v.Name] = v.Default
	}

	sc.env = map[string]string{}
	if withEnv {
		// Override default with values from environment.
		for _, v := range os.Environ() {
			parts := strings.SplitN(v, "=", 2)
			name, value := parts[0], parts[1]
			sc.env[name] = value
		}
	}

	sc.values = map[string]cty.Value{}
	sc.progress = map[string]struct{}{}

	for k := range sc.attrs {
		if _, err := sc.resolveValue(k); err != nil {
			return nil, err
		}
	}

	for k := range sc.defaults {
		if _, err := sc.resolveValue(k); err != nil {
			return nil, err
		}
	}
	return sc.values, nil
}

func (sc *StaticConfig) resolveValue(name string) (v *cty.Value, err error) {
	if v, ok := sc.values[name]; ok {
		return &v, nil
	}
	if _, ok := sc.progress[name]; ok {
		return nil, errors.Errorf("variable cycle not allowed")
	}
	sc.progress[name] = struct{}{}

	defer func() {
		if v != nil {
			sc.values[name] = *v
		}
	}()

	def, ok := sc.attrs[name]
	if !ok {
		def, ok = sc.defaults[name]
		if !ok {
			return nil, errors.Errorf("undefined variable %q", name)
		}
	}

	if def == nil {
		v := cty.StringVal(sc.env[name])
		return &v, nil
	}

	ectx := &hcl.EvalContext{
		Variables: map[string]cty.Value{},
		Functions: stdlibFunctions, // user functions not possible atm
	}
	for _, v := range def.Expr.Variables() {
		value, err := sc.resolveValue(v.RootName())
		if err != nil {
			var diags hcl.Diagnostics
			if !errors.As(err, &diags) {
				return nil, err
			}
			r := v.SourceRange()
			return nil, hcl.Diagnostics{
				&hcl.Diagnostic{
					Severity: hcl.DiagError,
					Summary:  "Invalid expression",
					Detail:   err.Error(),
					Subject:  &r,
					Context:  &r,
				},
			}
		}
		ectx.Variables[v.RootName()] = *value
	}

	vv, diags := def.Expr.Value(ectx)
	if diags.HasErrors() {
		return nil, diags
	}

	_, isVar := sc.defaults[name]

	if envv, ok := sc.env[name]; ok && isVar {
		if vv.Type().Equals(cty.Bool) {
			b, err := strconv.ParseBool(envv)
			if err != nil {
				return nil, errors.Wrapf(err, "failed to parse %s as bool", name)
			}
			v := cty.BoolVal(b)
			return &v, nil
		} else if vv.Type().Equals(cty.String) {
			v := cty.StringVal(envv)
			return &v, nil
		} else if vv.Type().Equals(cty.Number) {
			n, err := strconv.ParseFloat(envv, 64)
			if err == nil && (math.IsNaN(n) || math.IsInf(n, 0)) {
				err = errors.Errorf("invalid number value")
			}
			if err != nil {
				return nil, errors.Wrapf(err, "failed to parse %s as number", name)
			}
			v := cty.NumberVal(big.NewFloat(n))
			return &v, nil
		} else {
			// TODO: support lists with csv values
			return nil, errors.Errorf("unsupported type %s for variable %s", v.Type(), name)
		}
	}
	return &vv, nil
}

func ParseHCLFile(dt []byte, fn string) (*hcl.File, *StaticConfig, error) {
	if strings.HasSuffix(fn, ".json") || strings.HasSuffix(fn, ".hcl") {
		return parseHCLFile(dt, fn)
	}
	f, sc, err := parseHCLFile(dt, fn+".hcl")
	if err != nil {
		f, sc, err2 := parseHCLFile(dt, fn+".json")
		if err2 == nil {
			return f, sc, nil
		}
	}
	return f, sc, err
}

func parseHCLFile(dt []byte, fn string) (f *hcl.File, _ *StaticConfig, err error) {
	defer func() {
		err = formatHCLError(dt, err)
	}()

	// Decode user defined functions, first parsing as hcl and falling back to
	// json, returning errors based on the file suffix.
	f, hcldiags := hclsyntax.ParseConfig(dt, fn, hcl.Pos{Line: 1, Column: 1})
	if hcldiags.HasErrors() {
		var jsondiags hcl.Diagnostics
		f, jsondiags = hcljson.Parse(dt, fn)
		if jsondiags.HasErrors() {
			fnl := strings.ToLower(fn)
			if strings.HasSuffix(fnl, ".json") {
				return nil, nil, jsondiags
			}
			return nil, nil, hcldiags
		}
	}

	var sc StaticConfig
	// Decode only variable blocks without interpolation.
	if err := gohcl.DecodeBody(f.Body, nil, &sc); err != nil {
		return nil, nil, err
	}

	attrs, diags := f.Body.JustAttributes()
	if diags.HasErrors() {
		for _, d := range diags {
			if d.Detail != "Blocks are not allowed here." {
				return nil, nil, diags
			}
		}
	}
	sc.attrs = attrs

	return f, &sc, nil
}

func ParseHCL(b hcl.Body, sc *StaticConfig) (_ *Config, err error) {

	// evaluate variables
	variables, err := sc.Values(true)
	if err != nil {
		return nil, err
	}

	userFunctions, _, diags := userfunc.DecodeUserFunctions(b, "function", func() *hcl.EvalContext {
		return &hcl.EvalContext{
			Functions: stdlibFunctions,
			Variables: variables,
		}
	})
	if diags.HasErrors() {
		return nil, diags
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
	if err := gohcl.DecodeBody(b, ctx, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func formatHCLError(dt []byte, err error) error {
	if err == nil {
		return nil
	}
	diags, ok := err.(hcl.Diagnostics)
	if !ok {
		return err
	}
	for _, d := range diags {
		if d.Severity != hcl.DiagError {
			continue
		}
		if d.Subject != nil {
			src := errdefs.Source{
				Info: &pb.SourceInfo{
					Filename: d.Subject.Filename,
					Data:     dt,
				},
				Ranges: []*pb.Range{toErrRange(d.Subject)},
			}
			err = errdefs.WithSource(err, src)
			break
		}
	}
	return err
}

func toErrRange(in *hcl.Range) *pb.Range {
	return &pb.Range{
		Start: pb.Position{Line: int32(in.Start.Line), Character: int32(in.Start.Column)},
		End:   pb.Position{Line: int32(in.End.Line), Character: int32(in.End.Column)},
	}
}
