package bake

import (
	"math"
	"math/big"
	"os"
	"reflect"
	"strconv"
	"strings"
	"unsafe"

	"github.com/docker/buildx/util/userfunc"
	"github.com/hashicorp/go-cty-funcs/cidr"
	"github.com/hashicorp/go-cty-funcs/crypto"
	"github.com/hashicorp/go-cty-funcs/encoding"
	"github.com/hashicorp/go-cty-funcs/uuid"
	hcl "github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/ext/tryfunc"
	"github.com/hashicorp/hcl/v2/ext/typeexpr"
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
	Functions []*Function `hcl:"function,block"`
	Remain    hcl.Body    `hcl:",remain"`

	attrs hcl.Attributes

	defaults  map[string]*hcl.Attribute
	funcDefs  map[string]*Function
	funcs     map[string]function.Function
	env       map[string]string
	ectx      hcl.EvalContext
	progress  map[string]struct{}
	progressF map[string]struct{}
}

func mergeStaticConfig(scs []*StaticConfig) *StaticConfig {
	if len(scs) == 0 {
		return nil
	}
	sc := scs[0]
	for _, s := range scs[1:] {
		sc.Variables = append(sc.Variables, s.Variables...)
		sc.Functions = append(sc.Functions, s.Functions...)
		for k, v := range s.attrs {
			sc.attrs[k] = v
		}
	}
	return sc
}

func (sc *StaticConfig) EvalContext(withEnv bool) (*hcl.EvalContext, error) {
	// json parser also parses blocks as attributes
	delete(sc.attrs, "target")
	delete(sc.attrs, "function")

	sc.defaults = map[string]*hcl.Attribute{}
	for _, v := range sc.Variables {
		if v.Name == "target" {
			continue
		}
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

	sc.funcDefs = map[string]*Function{}
	for _, v := range sc.Functions {
		sc.funcDefs[v.Name] = v
	}

	sc.ectx = hcl.EvalContext{
		Variables: map[string]cty.Value{},
		Functions: stdlibFunctions,
	}
	sc.funcs = map[string]function.Function{}
	sc.progress = map[string]struct{}{}
	sc.progressF = map[string]struct{}{}
	for k := range sc.attrs {
		if err := sc.resolveValue(k); err != nil {
			return nil, err
		}
	}

	for k := range sc.defaults {
		if err := sc.resolveValue(k); err != nil {
			return nil, err
		}
	}

	for k := range sc.funcDefs {
		if err := sc.resolveFunction(k); err != nil {
			return nil, err
		}
	}
	return &sc.ectx, nil
}

type jsonExp interface {
	ExprList() []hcl.Expression
	ExprMap() []hcl.KeyValuePair
}

func elementExpressions(je jsonExp, exp hcl.Expression) []hcl.Expression {
	list := je.ExprList()
	if len(list) != 0 {
		exp := make([]hcl.Expression, 0, len(list))
		for _, e := range list {
			if je, ok := e.(jsonExp); ok {
				exp = append(exp, elementExpressions(je, e)...)
			}
		}
		return exp
	}
	kvlist := je.ExprMap()
	if len(kvlist) != 0 {
		exp := make([]hcl.Expression, 0, len(kvlist)*2)
		for _, p := range kvlist {
			exp = append(exp, p.Key)
			if je, ok := p.Value.(jsonExp); ok {
				exp = append(exp, elementExpressions(je, p.Value)...)
			}
		}
		return exp
	}
	return []hcl.Expression{exp}
}

func jsonFuncCallsRecursive(exp hcl.Expression) ([]string, error) {
	je, ok := exp.(jsonExp)
	if !ok {
		return nil, errors.Errorf("invalid expression type %T", exp)
	}
	m := map[string]struct{}{}
	for _, e := range elementExpressions(je, exp) {
		if err := appendJSONFuncCalls(e, m); err != nil {
			return nil, err
		}
	}
	arr := make([]string, 0, len(m))
	for n := range m {
		arr = append(arr, n)
	}
	return arr, nil
}

func appendJSONFuncCalls(exp hcl.Expression, m map[string]struct{}) error {
	v := reflect.ValueOf(exp)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		return errors.Errorf("invalid json expression kind %T %v", exp, v.Kind())
	}
	if v.Elem().Kind() != reflect.Struct {
		return errors.Errorf("invalid json expression pointer to %T %v", exp, v.Elem().Kind())
	}
	src := v.Elem().FieldByName("src")
	if src.IsZero() {
		return errors.Errorf("%v has no property src", v.Elem().Type())
	}
	if src.Kind() != reflect.Interface {
		return errors.Errorf("%v src is not interface: %v", src.Type(), src.Kind())
	}
	src = src.Elem()
	if src.IsNil() {
		return nil
	}
	if src.Kind() == reflect.Ptr {
		src = src.Elem()
	}
	if src.Kind() != reflect.Struct {
		return errors.Errorf("%v is not struct: %v", src.Type(), src.Kind())
	}

	// hcl/v2/json/ast#stringVal
	val := src.FieldByName("Value")
	if val.IsZero() {
		return nil
	}
	rng := src.FieldByName("SrcRange")
	if val.IsZero() {
		return nil
	}
	var stringVal struct {
		Value    string
		SrcRange hcl.Range
	}

	if !val.Type().AssignableTo(reflect.ValueOf(stringVal.Value).Type()) {
		return nil
	}
	if !rng.Type().AssignableTo(reflect.ValueOf(stringVal.SrcRange).Type()) {
		return nil
	}
	// reflect.Set does not work for unexported fields
	stringVal.Value = *(*string)(unsafe.Pointer(val.UnsafeAddr()))
	stringVal.SrcRange = *(*hcl.Range)(unsafe.Pointer(rng.UnsafeAddr()))

	expr, diags := hclsyntax.ParseExpression([]byte(stringVal.Value), stringVal.SrcRange.Filename, stringVal.SrcRange.Start)
	if diags.HasErrors() {
		return nil
	}

	fns, err := funcCalls(expr)
	if err != nil {
		return err
	}

	for _, fn := range fns {
		m[fn] = struct{}{}
	}

	return nil
}

func funcCalls(exp hcl.Expression) ([]string, hcl.Diagnostics) {
	node, ok := exp.(hclsyntax.Node)
	if !ok {
		fns, err := jsonFuncCallsRecursive(exp)
		if err != nil {
			return nil, hcl.Diagnostics{
				&hcl.Diagnostic{
					Severity: hcl.DiagError,
					Summary:  "Invalid expression",
					Detail:   err.Error(),
					Subject:  exp.Range().Ptr(),
					Context:  exp.Range().Ptr(),
				},
			}
		}
		return fns, nil
	}

	var funcnames []string
	hcldiags := hclsyntax.VisitAll(node, func(n hclsyntax.Node) hcl.Diagnostics {
		if fe, ok := n.(*hclsyntax.FunctionCallExpr); ok {
			funcnames = append(funcnames, fe.Name)
		}
		return nil
	})
	if hcldiags.HasErrors() {
		return nil, hcldiags
	}
	return funcnames, nil
}

func (sc *StaticConfig) loadDeps(exp hcl.Expression, exclude map[string]struct{}) hcl.Diagnostics {
	fns, hcldiags := funcCalls(exp)
	if hcldiags.HasErrors() {
		return hcldiags
	}

	for _, fn := range fns {
		if err := sc.resolveFunction(fn); err != nil {
			return hcl.Diagnostics{
				&hcl.Diagnostic{
					Severity: hcl.DiagError,
					Summary:  "Invalid expression",
					Detail:   err.Error(),
					Subject:  exp.Range().Ptr(),
					Context:  exp.Range().Ptr(),
				},
			}
		}
	}

	for _, v := range exp.Variables() {
		if _, ok := exclude[v.RootName()]; ok {
			continue
		}
		if err := sc.resolveValue(v.RootName()); err != nil {
			return hcl.Diagnostics{
				&hcl.Diagnostic{
					Severity: hcl.DiagError,
					Summary:  "Invalid expression",
					Detail:   err.Error(),
					Subject:  v.SourceRange().Ptr(),
					Context:  v.SourceRange().Ptr(),
				},
			}
		}
	}

	return nil
}

func (sc *StaticConfig) resolveFunction(name string) error {
	if _, ok := sc.funcs[name]; ok {
		return nil
	}
	f, ok := sc.funcDefs[name]
	if !ok {
		if _, ok := sc.ectx.Functions[name]; ok {
			return nil
		}
		return errors.Errorf("undefined function %s", name)
	}
	if _, ok := sc.progressF[name]; ok {
		return errors.Errorf("function cycle not allowed for %s", name)
	}
	sc.progressF[name] = struct{}{}

	paramExprs, paramsDiags := hcl.ExprList(f.Params.Expr)
	if paramsDiags.HasErrors() {
		return paramsDiags
	}
	var diags hcl.Diagnostics
	params := map[string]struct{}{}
	for _, paramExpr := range paramExprs {
		param := hcl.ExprAsKeyword(paramExpr)
		if param == "" {
			diags = append(diags, &hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  "Invalid param element",
				Detail:   "Each parameter name must be an identifier.",
				Subject:  paramExpr.Range().Ptr(),
			})
		}
		params[param] = struct{}{}
	}
	var variadic hcl.Expression
	if f.Variadic != nil {
		variadic = f.Variadic.Expr
		param := hcl.ExprAsKeyword(variadic)
		if param == "" {
			diags = append(diags, &hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  "Invalid param element",
				Detail:   "Each parameter name must be an identifier.",
				Subject:  f.Variadic.Range.Ptr(),
			})
		}
		params[param] = struct{}{}
	}
	if diags.HasErrors() {
		return diags
	}

	if diags := sc.loadDeps(f.Result.Expr, params); diags.HasErrors() {
		return diags
	}

	v, diags := userfunc.NewFunction(f.Params.Expr, variadic, f.Result.Expr, func() *hcl.EvalContext {
		return &sc.ectx
	})
	if diags.HasErrors() {
		return diags
	}
	sc.funcs[name] = v
	sc.ectx.Functions[name] = v

	return nil
}

func (sc *StaticConfig) resolveValue(name string) (err error) {
	if _, ok := sc.ectx.Variables[name]; ok {
		return nil
	}
	if _, ok := sc.progress[name]; ok {
		return errors.Errorf("variable cycle not allowed for %s", name)
	}
	sc.progress[name] = struct{}{}

	var v *cty.Value
	defer func() {
		if v != nil {
			sc.ectx.Variables[name] = *v
		}
	}()

	def, ok := sc.attrs[name]
	if !ok {
		def, ok = sc.defaults[name]
		if !ok {
			return errors.Errorf("undefined variable %q", name)
		}
	}

	if def == nil {
		vv := cty.StringVal(sc.env[name])
		v = &vv
		return
	}

	if diags := sc.loadDeps(def.Expr, nil); diags.HasErrors() {
		return diags
	}
	vv, diags := def.Expr.Value(&sc.ectx)
	if diags.HasErrors() {
		return diags
	}

	_, isVar := sc.defaults[name]

	if envv, ok := sc.env[name]; ok && isVar {
		if vv.Type().Equals(cty.Bool) {
			b, err := strconv.ParseBool(envv)
			if err != nil {
				return errors.Wrapf(err, "failed to parse %s as bool", name)
			}
			vv := cty.BoolVal(b)
			v = &vv
			return nil
		} else if vv.Type().Equals(cty.String) {
			vv := cty.StringVal(envv)
			v = &vv
			return nil
		} else if vv.Type().Equals(cty.Number) {
			n, err := strconv.ParseFloat(envv, 64)
			if err == nil && (math.IsNaN(n) || math.IsInf(n, 0)) {
				err = errors.Errorf("invalid number value")
			}
			if err != nil {
				return errors.Wrapf(err, "failed to parse %s as number", name)
			}
			vv := cty.NumberVal(big.NewFloat(n))
			v = &vv
			return nil
		} else {
			// TODO: support lists with csv values
			return errors.Errorf("unsupported type %s for variable %s", v.Type(), name)
		}
	}
	v = &vv
	return nil
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
	ctx, err := sc.EvalContext(true)
	if err != nil {
		return nil, err
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
