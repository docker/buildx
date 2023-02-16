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
	"github.com/zclconf/go-cty/cty/gocty"
)

type Opt struct {
	LookupVar     func(string) (string, bool)
	Vars          map[string]string
	ValidateLabel func(string) error
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

	blocks      map[string]map[string][]*hcl.Block
	blockValues map[*hcl.Block]reflect.Value
	blockTypes  map[string]reflect.Type

	ectx *hcl.EvalContext

	progress  map[string]struct{}
	progressF map[string]struct{}
	progressB map[*hcl.Block]map[string]struct{}
	doneF     map[string]struct{}
	doneB     map[*hcl.Block]map[string]struct{}
}

var errUndefined = errors.New("undefined")

func (p *parser) loadDeps(exp hcl.Expression, exclude map[string]struct{}, allowMissing bool) hcl.Diagnostics {
	fns, hcldiags := funcCalls(exp)
	if hcldiags.HasErrors() {
		return hcldiags
	}

	for _, fn := range fns {
		if err := p.resolveFunction(fn); err != nil {
			if allowMissing && errors.Is(err, errUndefined) {
				continue
			}
			return wrapErrorDiagnostic("Invalid expression", err, exp.Range().Ptr(), exp.Range().Ptr())
		}
	}

	for _, v := range exp.Variables() {
		if _, ok := exclude[v.RootName()]; ok {
			continue
		}
		if _, ok := p.blockTypes[v.RootName()]; ok {
			blockType := v.RootName()

			split := v.SimpleSplit().Rel
			if len(split) == 0 {
				return hcl.Diagnostics{
					&hcl.Diagnostic{
						Severity: hcl.DiagError,
						Summary:  "Invalid expression",
						Detail:   fmt.Sprintf("cannot access %s as a variable", blockType),
						Subject:  exp.Range().Ptr(),
						Context:  exp.Range().Ptr(),
					},
				}
			}
			blockName, ok := split[0].(hcl.TraverseAttr)
			if !ok {
				return hcl.Diagnostics{
					&hcl.Diagnostic{
						Severity: hcl.DiagError,
						Summary:  "Invalid expression",
						Detail:   fmt.Sprintf("cannot traverse %s without attribute", blockType),
						Subject:  exp.Range().Ptr(),
						Context:  exp.Range().Ptr(),
					},
				}
			}
			blocks := p.blocks[blockType][blockName.Name]
			if len(blocks) == 0 {
				continue
			}

			var target *hcl.BodySchema
			if len(split) > 1 {
				if attr, ok := split[1].(hcl.TraverseAttr); ok {
					target = &hcl.BodySchema{
						Attributes: []hcl.AttributeSchema{{Name: attr.Name}},
						Blocks:     []hcl.BlockHeaderSchema{{Type: attr.Name}},
					}
				}
			}
			if err := p.resolveBlock(blocks[0], target); err != nil {
				if allowMissing && errors.Is(err, errUndefined) {
					continue
				}
				return wrapErrorDiagnostic("Invalid expression", err, exp.Range().Ptr(), exp.Range().Ptr())
			}
		} else {
			if err := p.resolveValue(v.RootName()); err != nil {
				if allowMissing && errors.Is(err, errUndefined) {
					continue
				}
				return wrapErrorDiagnostic("Invalid expression", err, exp.Range().Ptr(), exp.Range().Ptr())
			}
		}
	}

	return nil
}

// resolveFunction forces evaluation of a function, storing the result into the
// parser.
func (p *parser) resolveFunction(name string) error {
	if _, ok := p.doneF[name]; ok {
		return nil
	}
	f, ok := p.funcs[name]
	if !ok {
		if _, ok := p.ectx.Functions[name]; ok {
			return nil
		}
		return errors.Wrapf(errUndefined, "function %q does not exit", name)
	}
	if _, ok := p.progressF[name]; ok {
		return errors.Errorf("function cycle not allowed for %s", name)
	}
	p.progressF[name] = struct{}{}

	if f.Result == nil {
		return errors.Errorf("empty result not allowed for %s", name)
	}
	if f.Params == nil {
		return errors.Errorf("empty params not allowed for %s", name)
	}

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

	if diags := p.loadDeps(f.Result.Expr, params, false); diags.HasErrors() {
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

// resolveValue forces evaluation of a named value, storing the result into the
// parser.
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
			return errors.Wrapf(errUndefined, "variable %q does not exit", name)
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

	if diags := p.loadDeps(def.Expr, nil, true); diags.HasErrors() {
		return diags
	}
	vv, diags := def.Expr.Value(p.ectx)
	if diags.HasErrors() {
		return diags
	}

	_, isVar := p.vars[name]

	if envv, ok := p.opt.LookupVar(name); ok && isVar {
		switch {
		case vv.Type().Equals(cty.Bool):
			b, err := strconv.ParseBool(envv)
			if err != nil {
				return errors.Wrapf(err, "failed to parse %s as bool", name)
			}
			vv = cty.BoolVal(b)
		case vv.Type().Equals(cty.String), vv.Type().Equals(cty.DynamicPseudoType):
			vv = cty.StringVal(envv)
		case vv.Type().Equals(cty.Number):
			n, err := strconv.ParseFloat(envv, 64)
			if err == nil && (math.IsNaN(n) || math.IsInf(n, 0)) {
				err = errors.Errorf("invalid number value")
			}
			if err != nil {
				return errors.Wrapf(err, "failed to parse %s as number", name)
			}
			vv = cty.NumberVal(big.NewFloat(n))
		default:
			// TODO: support lists with csv values
			return errors.Errorf("unsupported type %s for variable %s", vv.Type().FriendlyName(), name)
		}
	}
	v = &vv
	return nil
}

// resolveBlock force evaluates a block, storing the result in the parser. If a
// target schema is provided, only the attributes and blocks present in the
// schema will be evaluated.
func (p *parser) resolveBlock(block *hcl.Block, target *hcl.BodySchema) (err error) {
	name := block.Labels[0]
	if err := p.opt.ValidateLabel(name); err != nil {
		return wrapErrorDiagnostic("Invalid name", err, &block.LabelRanges[0], &block.LabelRanges[0])
	}

	if _, ok := p.doneB[block]; !ok {
		p.doneB[block] = map[string]struct{}{}
	}
	if _, ok := p.progressB[block]; !ok {
		p.progressB[block] = map[string]struct{}{}
	}

	if target != nil {
		// filter out attributes and blocks that are already evaluated
		original := target
		target = &hcl.BodySchema{}
		for _, a := range original.Attributes {
			if _, ok := p.doneB[block][a.Name]; !ok {
				target.Attributes = append(target.Attributes, a)
			}
		}
		for _, b := range original.Blocks {
			if _, ok := p.doneB[block][b.Type]; !ok {
				target.Blocks = append(target.Blocks, b)
			}
		}
		if len(target.Attributes) == 0 && len(target.Blocks) == 0 {
			return nil
		}
	}

	if target != nil {
		// detect reference cycles
		for _, a := range target.Attributes {
			if _, ok := p.progressB[block][a.Name]; ok {
				return errors.Errorf("reference cycle not allowed for %s.%s.%s", block.Type, name, a.Name)
			}
		}
		for _, b := range target.Blocks {
			if _, ok := p.progressB[block][b.Type]; ok {
				return errors.Errorf("reference cycle not allowed for %s.%s.%s", block.Type, name, b.Type)
			}
		}
		for _, a := range target.Attributes {
			p.progressB[block][a.Name] = struct{}{}
		}
		for _, b := range target.Blocks {
			p.progressB[block][b.Type] = struct{}{}
		}
	}

	// create a filtered body that contains only the target properties
	body := func() hcl.Body {
		if target != nil {
			return FilterIncludeBody(block.Body, target)
		}

		filter := &hcl.BodySchema{}
		for k := range p.doneB[block] {
			filter.Attributes = append(filter.Attributes, hcl.AttributeSchema{Name: k})
			filter.Blocks = append(filter.Blocks, hcl.BlockHeaderSchema{Type: k})
		}
		return FilterExcludeBody(block.Body, filter)
	}

	// load dependencies from all targeted properties
	t, ok := p.blockTypes[block.Type]
	if !ok {
		return nil
	}
	schema, _ := gohcl.ImpliedBodySchema(reflect.New(t).Interface())
	content, _, diag := body().PartialContent(schema)
	if diag.HasErrors() {
		return diag
	}
	for _, a := range content.Attributes {
		diag := p.loadDeps(a.Expr, nil, true)
		if diag.HasErrors() {
			return diag
		}
	}
	for _, b := range content.Blocks {
		err := p.resolveBlock(b, nil)
		if err != nil {
			return err
		}
	}

	// decode!
	var output reflect.Value
	if prev, ok := p.blockValues[block]; ok {
		output = prev
	} else {
		output = reflect.New(t)
		setLabel(output, block.Labels[0]) // early attach labels, so we can reference them
	}
	diag = gohcl.DecodeBody(body(), p.ectx, output.Interface())
	if diag.HasErrors() {
		return diag
	}
	p.blockValues[block] = output

	// mark all targeted properties as done
	for _, a := range content.Attributes {
		p.doneB[block][a.Name] = struct{}{}
	}
	for _, b := range content.Blocks {
		p.doneB[block][b.Type] = struct{}{}
	}
	if target != nil {
		for _, a := range target.Attributes {
			p.doneB[block][a.Name] = struct{}{}
		}
		for _, b := range target.Blocks {
			p.doneB[block][b.Type] = struct{}{}
		}
	}

	// store the result into the evaluation context (so if can be referenced)
	outputType, err := gocty.ImpliedType(output.Interface())
	if err != nil {
		return err
	}
	outputValue, err := gocty.ToCtyValue(output.Interface(), outputType)
	if err != nil {
		return err
	}
	var m map[string]cty.Value
	if m2, ok := p.ectx.Variables[block.Type]; ok {
		m = m2.AsValueMap()
	}
	if m == nil {
		m = map[string]cty.Value{}
	}
	m[name] = outputValue
	p.ectx.Variables[block.Type] = cty.MapVal(m)

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
	defsSchema, _ := gohcl.ImpliedBodySchema(defs)

	if opt.LookupVar == nil {
		opt.LookupVar = func(string) (string, bool) {
			return "", false
		}
	}

	if opt.ValidateLabel == nil {
		opt.ValidateLabel = func(string) error {
			return nil
		}
	}

	p := &parser{
		opt: opt,

		vars:  map[string]*variable{},
		attrs: map[string]*hcl.Attribute{},
		funcs: map[string]*functionDef{},

		blocks:      map[string]map[string][]*hcl.Block{},
		blockValues: map[*hcl.Block]reflect.Value{},
		blockTypes:  map[string]reflect.Type{},

		progress:  map[string]struct{}{},
		progressF: map[string]struct{}{},
		progressB: map[*hcl.Block]map[string]struct{}{},

		doneF: map[string]struct{}{},
		doneB: map[*hcl.Block]map[string]struct{}{},
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

	content, b, diags := b.PartialContent(schema)
	if diags.HasErrors() {
		return diags
	}

	blocks, b, diags := b.PartialContent(defsSchema)
	if diags.HasErrors() {
		return diags
	}

	attrs, diags := b.JustAttributes()
	if diags.HasErrors() {
		if d := removeAttributesDiags(diags, reserved, p.vars); len(d) > 0 {
			return d
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

	for k := range p.vars {
		if err := p.resolveValue(k); err != nil {
			if diags, ok := err.(hcl.Diagnostics); ok {
				return diags
			}
			r := p.vars[k].Body.MissingItemRange()
			return wrapErrorDiagnostic("Invalid value", err, &r, &r)
		}
	}

	for k := range p.funcs {
		if err := p.resolveFunction(k); err != nil {
			if diags, ok := err.(hcl.Diagnostics); ok {
				return diags
			}
			var subject *hcl.Range
			var context *hcl.Range
			if p.funcs[k].Params != nil {
				subject = &p.funcs[k].Params.Range
				context = subject
			} else {
				for _, block := range blocks.Blocks {
					if block.Type == "function" && len(block.Labels) == 1 && block.Labels[0] == k {
						subject = &block.LabelRanges[0]
						context = &block.DefRange
						break
					}
				}
			}
			return wrapErrorDiagnostic("Invalid function", err, subject, context)
		}
	}

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
		bm, ok := p.blocks[b.Type]
		if !ok {
			bm = map[string][]*hcl.Block{}
			p.blocks[b.Type] = bm
		}

		lbl := b.Labels[0]
		bm[lbl] = append(bm[lbl], b)
	}

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

	vt := reflect.ValueOf(val).Elem().Type()
	for i := 0; i < vt.NumField(); i++ {
		tags := strings.Split(vt.Field(i).Tag.Get("hcl"), ",")

		p.blockTypes[tags[0]] = vt.Field(i).Type.Elem().Elem()
		types[tags[0]] = field{
			idx:    i,
			typ:    vt.Field(i).Type,
			values: make(map[string]value),
		}
	}

	diags = hcl.Diagnostics{}
	for _, b := range content.Blocks {
		v := reflect.ValueOf(val)

		err := p.resolveBlock(b, nil)
		if err != nil {
			if diag, ok := err.(hcl.Diagnostics); ok {
				if diag.HasErrors() {
					diags = append(diags, diag...)
					continue
				}
			} else {
				return wrapErrorDiagnostic("Invalid block", err, &b.LabelRanges[0], &b.DefRange)
			}
		}

		vv := p.blockValues[b]

		t := types[b.Type]
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

	for k := range p.attrs {
		if err := p.resolveValue(k); err != nil {
			if diags, ok := err.(hcl.Diagnostics); ok {
				return diags
			}
			return wrapErrorDiagnostic("Invalid attribute", err, &p.attrs[k].Range, &p.attrs[k].Range)
		}
	}

	return nil
}

// wrapErrorDiagnostic wraps an error into a hcl.Diagnostics object.
// If the error is already an hcl.Diagnostics object, it is returned as is.
func wrapErrorDiagnostic(message string, err error, subject *hcl.Range, context *hcl.Range) hcl.Diagnostics {
	switch err := err.(type) {
	case *hcl.Diagnostic:
		return hcl.Diagnostics{err}
	case hcl.Diagnostics:
		return err
	default:
		return hcl.Diagnostics{
			&hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  message,
				Detail:   err.Error(),
				Subject:  subject,
				Context:  context,
			},
		}
	}
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

func removeAttributesDiags(diags hcl.Diagnostics, reserved map[string]struct{}, vars map[string]*variable) hcl.Diagnostics {
	var fdiags hcl.Diagnostics
	for _, d := range diags {
		if fout := func(d *hcl.Diagnostic) bool {
			// https://github.com/docker/buildx/pull/541
			if d.Detail == "Blocks are not allowed here." {
				return true
			}
			for r := range reserved {
				// JSON body objects don't handle repeated blocks like HCL but
				// reserved name attributes should be allowed when multi bodies are merged.
				// https://github.com/hashicorp/hcl/blob/main/json/spec.md#blocks
				if strings.HasPrefix(d.Detail, fmt.Sprintf(`Argument "%s" was already set at `, r)) {
					return true
				}
			}
			for v := range vars {
				// Do the same for global variables
				if strings.HasPrefix(d.Detail, fmt.Sprintf(`Argument "%s" was already set at `, v)) {
					return true
				}
			}
			return false
		}(d); !fout {
			fdiags = append(fdiags, d)
		}
	}
	return fdiags
}
