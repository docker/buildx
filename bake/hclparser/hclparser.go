package hclparser

import (
	"fmt"
	"math"
	"math/big"
	"reflect"
	"strconv"
	"strings"

	"github.com/docker/buildx/util/userfunc"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/pkg/errors"
	"github.com/zclconf/go-cty/cty"
)

type Opt struct {
	LookupVar func(string) (string, bool)
	Vars      map[string]string
}

type variable struct {
	Name    string         `json:"-" hcl:"name,label"`
	Default *hcl.Attribute `json:"default,omitempty" hcl:"default,optional"`
	Body    hcl.Body       `json:"-" hcl:",body"`
}

type functionDef struct {
	Name     string         `json:"-" hcl:"name,label"`
	Params   *hcl.Attribute `json:"params,omitempty" hcl:"params"`
	Variadic *hcl.Attribute `json:"variadic_param,omitempty" hcl:"variadic_params"`
	Result   *hcl.Attribute `json:"result,omitempty" hcl:"result"`
}

type inputs struct {
	Variables []*variable    `hcl:"variable,block"`
	Functions []*functionDef `hcl:"function,block"`

	Remain hcl.Body `json:"-" hcl:",remain"`
}

type parser struct {
	opt Opt

	vars  map[string]*variable
	attrs map[string]*hcl.Attribute
	funcs map[string]*functionDef

	ectx *hcl.EvalContext

	progress  map[string]struct{}
	progressF map[string]struct{}
	doneF     map[string]struct{}
}

func (p *parser) loadDeps(exp hcl.Expression, exclude map[string]struct{}) hcl.Diagnostics {
	fns, hcldiags := funcCalls(exp)
	if hcldiags.HasErrors() {
		return hcldiags
	}

	for _, fn := range fns {
		if err := p.resolveFunction(fn); err != nil {
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
		if err := p.resolveValue(v.RootName()); err != nil {
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

func (p *parser) resolveFunction(name string) error {
	if _, ok := p.doneF[name]; ok {
		return nil
	}
	f, ok := p.funcs[name]
	if !ok {
		if _, ok := p.ectx.Functions[name]; ok {
			return nil
		}
		return errors.Errorf("undefined function %s", name)
	}
	if _, ok := p.progressF[name]; ok {
		return errors.Errorf("function cycle not allowed for %s", name)
	}
	p.progressF[name] = struct{}{}

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

	if diags := p.loadDeps(f.Result.Expr, params); diags.HasErrors() {
		return diags
	}

	v, diags := userfunc.NewFunction(f.Params.Expr, variadic, f.Result.Expr, func() *hcl.EvalContext {
		return p.ectx
	})
	if diags.HasErrors() {
		return diags
	}
	p.doneF[name] = struct{}{}
	p.ectx.Functions[name] = v

	return nil
}

func (p *parser) resolveValue(name string) (err error) {
	if _, ok := p.ectx.Variables[name]; ok {
		return nil
	}
	if _, ok := p.progress[name]; ok {
		return errors.Errorf("variable cycle not allowed for %s", name)
	}
	p.progress[name] = struct{}{}

	var v *cty.Value
	defer func() {
		if v != nil {
			p.ectx.Variables[name] = *v
		}
	}()

	def, ok := p.attrs[name]
	if _, builtin := p.opt.Vars[name]; !ok && !builtin {
		vr, ok := p.vars[name]
		if !ok {
			return errors.Errorf("undefined variable %q", name)
		}
		def = vr.Default
	}

	if def == nil {
		val, ok := p.opt.Vars[name]
		if !ok {
			val, _ = p.opt.LookupVar(name)
		}
		vv := cty.StringVal(val)
		v = &vv
		return
	}

	if diags := p.loadDeps(def.Expr, nil); diags.HasErrors() {
		return diags
	}
	vv, diags := def.Expr.Value(p.ectx)
	if diags.HasErrors() {
		return diags
	}

	_, isVar := p.vars[name]

	if envv, ok := p.opt.LookupVar(name); ok && isVar {
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

func Parse(b hcl.Body, opt Opt, val interface{}) hcl.Diagnostics {
	reserved := map[string]struct{}{}
	schema, _ := gohcl.ImpliedBodySchema(val)

	for _, bs := range schema.Blocks {
		reserved[bs.Type] = struct{}{}
	}
	for k := range opt.Vars {
		reserved[k] = struct{}{}
	}

	var defs inputs
	if err := gohcl.DecodeBody(b, nil, &defs); err != nil {
		return err
	}

	if opt.LookupVar == nil {
		opt.LookupVar = func(string) (string, bool) {
			return "", false
		}
	}

	p := &parser{
		opt: opt,

		vars:  map[string]*variable{},
		attrs: map[string]*hcl.Attribute{},
		funcs: map[string]*functionDef{},

		progress:  map[string]struct{}{},
		progressF: map[string]struct{}{},
		doneF:     map[string]struct{}{},
		ectx: &hcl.EvalContext{
			Variables: map[string]cty.Value{},
			Functions: stdlibFunctions,
		},
	}

	for _, v := range defs.Variables {
		// TODO: validate name
		if _, ok := reserved[v.Name]; ok {
			continue
		}
		p.vars[v.Name] = v
	}
	for _, v := range defs.Functions {
		// TODO: validate name
		if _, ok := reserved[v.Name]; ok {
			continue
		}
		p.funcs[v.Name] = v
	}

	attrs, diags := b.JustAttributes()
	if diags.HasErrors() {
		for _, d := range diags {
			if d.Detail != "Blocks are not allowed here." {
				return diags
			}
		}
	}

	for _, v := range attrs {
		if _, ok := reserved[v.Name]; ok {
			continue
		}
		p.attrs[v.Name] = v
	}
	delete(p.attrs, "function")

	for k := range p.opt.Vars {
		_ = p.resolveValue(k)
	}

	for k := range p.attrs {
		if err := p.resolveValue(k); err != nil {
			if diags, ok := err.(hcl.Diagnostics); ok {
				return diags
			}
			return hcl.Diagnostics{
				&hcl.Diagnostic{
					Severity: hcl.DiagError,
					Summary:  "Invalid attribute",
					Detail:   err.Error(),
					Subject:  &p.attrs[k].Range,
					Context:  &p.attrs[k].Range,
				},
			}
		}
	}

	for k := range p.vars {
		if err := p.resolveValue(k); err != nil {
			if diags, ok := err.(hcl.Diagnostics); ok {
				return diags
			}
			r := p.vars[k].Body.MissingItemRange()
			return hcl.Diagnostics{
				&hcl.Diagnostic{
					Severity: hcl.DiagError,
					Summary:  "Invalid value",
					Detail:   err.Error(),
					Subject:  &r,
					Context:  &r,
				},
			}
		}
	}

	for k := range p.funcs {
		if err := p.resolveFunction(k); err != nil {
			if diags, ok := err.(hcl.Diagnostics); ok {
				return diags
			}
			return hcl.Diagnostics{
				&hcl.Diagnostic{
					Severity: hcl.DiagError,
					Summary:  "Invalid function",
					Detail:   err.Error(),
					Subject:  &p.funcs[k].Params.Range,
					Context:  &p.funcs[k].Params.Range,
				},
			}
		}
	}

	content, _, diags := b.PartialContent(schema)
	if diags.HasErrors() {
		return diags
	}

	for _, a := range content.Attributes {
		return hcl.Diagnostics{
			&hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  "Invalid attribute",
				Detail:   "global attributes currently not supported",
				Subject:  &a.Range,
				Context:  &a.Range,
			},
		}
	}

	m := map[string]map[string][]*hcl.Block{}
	for _, b := range content.Blocks {
		if len(b.Labels) == 0 || len(b.Labels) > 1 {
			return hcl.Diagnostics{
				&hcl.Diagnostic{
					Severity: hcl.DiagError,
					Summary:  "Invalid block",
					Detail:   fmt.Sprintf("invalid block label: %v", b.Labels),
					Subject:  &b.LabelRanges[0],
					Context:  &b.LabelRanges[0],
				},
			}
		}
		bm, ok := m[b.Type]
		if !ok {
			bm = map[string][]*hcl.Block{}
			m[b.Type] = bm
		}

		lbl := b.Labels[0]
		bm[lbl] = append(bm[lbl], b)
	}

	vt := reflect.ValueOf(val).Elem().Type()
	numFields := vt.NumField()

	type value struct {
		reflect.Value
		idx int
	}
	type field struct {
		idx    int
		typ    reflect.Type
		values map[string]value
	}
	types := map[string]field{}

	for i := 0; i < numFields; i++ {
		tags := strings.Split(vt.Field(i).Tag.Get("hcl"), ",")

		types[tags[0]] = field{
			idx:    i,
			typ:    vt.Field(i).Type,
			values: make(map[string]value),
		}
	}

	diags = hcl.Diagnostics{}
	for _, b := range content.Blocks {
		v := reflect.ValueOf(val)

		t, ok := types[b.Type]
		if !ok {
			continue
		}

		vv := reflect.New(t.typ.Elem().Elem())
		diag := gohcl.DecodeBody(b.Body, p.ectx, vv.Interface())
		if diag.HasErrors() {
			diags = append(diags, diag...)
			continue
		}

		lblIndex := setLabel(vv, b.Labels[0])

		oldValue, exists := t.values[b.Labels[0]]
		if !exists && lblIndex != -1 {
			if v.Elem().Field(t.idx).Type().Kind() == reflect.Slice {
				for i := 0; i < v.Elem().Field(t.idx).Len(); i++ {
					if b.Labels[0] == v.Elem().Field(t.idx).Index(i).Elem().Field(lblIndex).String() {
						exists = true
						oldValue = value{Value: v.Elem().Field(t.idx).Index(i), idx: i}
						break
					}
				}
			}

		}
		if exists {
			if m := oldValue.Value.MethodByName("Merge"); m.IsValid() {
				m.Call([]reflect.Value{vv})
			} else {
				v.Elem().Field(t.idx).Index(oldValue.idx).Set(vv)
			}
		} else {
			slice := v.Elem().Field(t.idx)
			if slice.IsNil() {
				slice = reflect.New(t.typ).Elem()
			}
			t.values[b.Labels[0]] = value{Value: vv, idx: slice.Len()}
			v.Elem().Field(t.idx).Set(reflect.Append(slice, vv))
		}
	}
	if diags.HasErrors() {
		return diags
	}

	return nil
}

func setLabel(v reflect.Value, lbl string) int {
	// cache field index?
	numFields := v.Elem().Type().NumField()
	for i := 0; i < numFields; i++ {
		for _, t := range strings.Split(v.Elem().Type().Field(i).Tag.Get("hcl"), ",") {
			if t == "label" {
				v.Elem().Field(i).Set(reflect.ValueOf(lbl))
				return i
			}
		}
	}
	return -1
}
