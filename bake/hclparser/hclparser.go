package hclparser

import (
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"math"
	"math/big"
	"reflect"
	"strconv"
	"strings"

	"github.com/docker/buildx/bake/hclparser/gohcl"
	"github.com/docker/buildx/util/userfunc"
	"github.com/hashicorp/hcl/v2"
	"github.com/pkg/errors"
	"github.com/zclconf/go-cty/cty"
)

type Opt struct {
	LookupVar     func(string) (string, bool)
	Vars          map[string]string
	ValidateLabel func(string) error
}

type variable struct {
	Name        string                `json:"-" hcl:"name,label"`
	Default     *hcl.Attribute        `json:"default,omitempty" hcl:"default,optional"`
	Description string                `json:"description,omitempty" hcl:"description,optional"`
	Validations []*variableValidation `json:"validation,omitempty" hcl:"validation,block"`
	Body        hcl.Body              `json:"-" hcl:",body"`
	Remain      hcl.Body              `json:"-" hcl:",remain"`
}

type variableValidation struct {
	Condition    hcl.Expression `json:"condition" hcl:"condition"`
	ErrorMessage hcl.Expression `json:"error_message" hcl:"error_message"`
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

	blocks       map[string]map[string][]*hcl.Block
	blockValues  map[*hcl.Block][]reflect.Value
	blockEvalCtx map[*hcl.Block][]*hcl.EvalContext
	blockNames   map[*hcl.Block][]string
	blockTypes   map[string]reflect.Type

	ectx *hcl.EvalContext

	progressV map[uint64]struct{}
	progressF map[uint64]struct{}
	progressB map[uint64]map[string]struct{}
	doneB     map[uint64]map[string]struct{}
}

type WithEvalContexts interface {
	GetEvalContexts(base *hcl.EvalContext, block *hcl.Block, loadDeps func(hcl.Expression) hcl.Diagnostics) ([]*hcl.EvalContext, error)
}

type WithGetName interface {
	GetName(ectx *hcl.EvalContext, block *hcl.Block, loadDeps func(hcl.Expression) hcl.Diagnostics) (string, error)
}

// errUndefined is returned when a variable or function is not defined.
type errUndefined struct{}

func (errUndefined) Error() string {
	return "undefined"
}

func (p *parser) loadDeps(ectx *hcl.EvalContext, exp hcl.Expression, exclude map[string]struct{}, allowMissing bool) hcl.Diagnostics {
	fns, hcldiags := funcCalls(exp)
	if hcldiags.HasErrors() {
		return hcldiags
	}

	for _, fn := range fns {
		if err := p.resolveFunction(ectx, fn); err != nil {
			if allowMissing && errors.Is(err, errUndefined{}) {
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
			for _, block := range blocks {
				if err := p.resolveBlock(block, target); err != nil {
					if allowMissing && errors.Is(err, errUndefined{}) {
						continue
					}
					return wrapErrorDiagnostic("Invalid expression", err, exp.Range().Ptr(), exp.Range().Ptr())
				}
			}
		} else {
			if err := p.resolveValue(ectx, v.RootName()); err != nil {
				if allowMissing && errors.Is(err, errUndefined{}) {
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
func (p *parser) resolveFunction(ectx *hcl.EvalContext, name string) error {
	if _, ok := p.ectx.Functions[name]; ok {
		return nil
	}
	if _, ok := ectx.Functions[name]; ok {
		return nil
	}
	f, ok := p.funcs[name]
	if !ok {
		return errors.Wrapf(errUndefined{}, "function %q does not exist", name)
	}
	if _, ok := p.progressF[key(ectx, name)]; ok {
		return errors.Errorf("function cycle not allowed for %s", name)
	}
	p.progressF[key(ectx, name)] = struct{}{}

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

	if diags := p.loadDeps(p.ectx, f.Result.Expr, params, false); diags.HasErrors() {
		return diags
	}

	v, diags := userfunc.NewFunction(f.Params.Expr, variadic, f.Result.Expr, func() *hcl.EvalContext {
		return p.ectx
	})
	if diags.HasErrors() {
		return diags
	}
	p.ectx.Functions[name] = v

	return nil
}

// resolveValue forces evaluation of a named value, storing the result into the
// parser.
func (p *parser) resolveValue(ectx *hcl.EvalContext, name string) (err error) {
	if _, ok := p.ectx.Variables[name]; ok {
		return nil
	}
	if _, ok := ectx.Variables[name]; ok {
		return nil
	}
	if _, ok := p.progressV[key(ectx, name)]; ok {
		return errors.Errorf("variable cycle not allowed for %s", name)
	}
	p.progressV[key(ectx, name)] = struct{}{}

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
			return errors.Wrapf(errUndefined{}, "variable %q does not exist", name)
		}
		def = vr.Default
		ectx = p.ectx
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

	if diags := p.loadDeps(ectx, def.Expr, nil, true); diags.HasErrors() {
		return diags
	}
	vv, diags := def.Expr.Value(ectx)
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
	// prepare the variable map for this type
	if _, ok := p.ectx.Variables[block.Type]; !ok {
		p.ectx.Variables[block.Type] = cty.MapValEmpty(cty.Map(cty.String))
	}

	// prepare the output destination and evaluation context
	t, ok := p.blockTypes[block.Type]
	if !ok {
		return nil
	}
	var outputs []reflect.Value
	var ectxs []*hcl.EvalContext
	if prev, ok := p.blockValues[block]; ok {
		outputs = prev
		ectxs = p.blockEvalCtx[block]
	} else {
		if v, ok := reflect.New(t).Interface().(WithEvalContexts); ok {
			ectxs, err = v.GetEvalContexts(p.ectx, block, func(expr hcl.Expression) hcl.Diagnostics {
				return p.loadDeps(p.ectx, expr, nil, true)
			})
			if err != nil {
				return err
			}
			for _, ectx := range ectxs {
				if ectx != p.ectx && ectx.Parent() != p.ectx {
					return errors.Errorf("EvalContext must return a context with the correct parent")
				}
			}
		} else {
			ectxs = append([]*hcl.EvalContext{}, p.ectx)
		}
		for range ectxs {
			outputs = append(outputs, reflect.New(t))
		}
	}
	p.blockValues[block] = outputs
	p.blockEvalCtx[block] = ectxs

	for i, output := range outputs {
		target := target
		ectx := ectxs[i]
		name := block.Labels[0]
		if names, ok := p.blockNames[block]; ok {
			name = names[i]
		}

		if _, ok := p.doneB[key(block, ectx)]; !ok {
			p.doneB[key(block, ectx)] = map[string]struct{}{}
		}
		if _, ok := p.progressB[key(block, ectx)]; !ok {
			p.progressB[key(block, ectx)] = map[string]struct{}{}
		}

		if target != nil {
			// filter out attributes and blocks that are already evaluated
			original := target
			target = &hcl.BodySchema{}
			for _, a := range original.Attributes {
				if _, ok := p.doneB[key(block, ectx)][a.Name]; !ok {
					target.Attributes = append(target.Attributes, a)
				}
			}
			for _, b := range original.Blocks {
				if _, ok := p.doneB[key(block, ectx)][b.Type]; !ok {
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
				if _, ok := p.progressB[key(block, ectx)][a.Name]; ok {
					return errors.Errorf("reference cycle not allowed for %s.%s.%s", block.Type, name, a.Name)
				}
			}
			for _, b := range target.Blocks {
				if _, ok := p.progressB[key(block, ectx)][b.Type]; ok {
					return errors.Errorf("reference cycle not allowed for %s.%s.%s", block.Type, name, b.Type)
				}
			}
			for _, a := range target.Attributes {
				p.progressB[key(block, ectx)][a.Name] = struct{}{}
			}
			for _, b := range target.Blocks {
				p.progressB[key(block, ectx)][b.Type] = struct{}{}
			}
		}

		// create a filtered body that contains only the target properties
		body := func() hcl.Body {
			if target != nil {
				return FilterIncludeBody(block.Body, target)
			}

			filter := &hcl.BodySchema{}
			for k := range p.doneB[key(block, ectx)] {
				filter.Attributes = append(filter.Attributes, hcl.AttributeSchema{Name: k})
				filter.Blocks = append(filter.Blocks, hcl.BlockHeaderSchema{Type: k})
			}
			return FilterExcludeBody(block.Body, filter)
		}

		// load dependencies from all targeted properties
		schema, _ := gohcl.ImpliedBodySchema(reflect.New(t).Interface())
		content, _, diag := body().PartialContent(schema)
		if diag.HasErrors() {
			return diag
		}
		for _, a := range content.Attributes {
			diag := p.loadDeps(ectx, a.Expr, nil, true)
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
		diag = decodeBody(body(), ectx, output.Interface())
		if diag.HasErrors() {
			return diag
		}

		// mark all targeted properties as done
		for _, a := range content.Attributes {
			p.doneB[key(block, ectx)][a.Name] = struct{}{}
		}
		for _, b := range content.Blocks {
			p.doneB[key(block, ectx)][b.Type] = struct{}{}
		}
		if target != nil {
			for _, a := range target.Attributes {
				p.doneB[key(block, ectx)][a.Name] = struct{}{}
			}
			for _, b := range target.Blocks {
				p.doneB[key(block, ectx)][b.Type] = struct{}{}
			}
		}

		// store the result into the evaluation context (so it can be referenced)
		outputType, err := ImpliedType(output.Interface())
		if err != nil {
			return err
		}
		outputValue, err := ToCtyValue(output.Interface(), outputType)
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

		// The logical contents of this structure is similar to a map,
		// but it's possible for some attributes to be different in a way that's
		// illegal for a map so we use an object here instead which is structurally
		// equivalent but allows disparate types for different keys.
		p.ectx.Variables[block.Type] = cty.ObjectVal(m)
	}

	return nil
}

// resolveBlockNames returns the names of the block, calling resolveBlock to
// evaluate any label fields to correctly resolve the name.
func (p *parser) resolveBlockNames(block *hcl.Block) ([]string, error) {
	if names, ok := p.blockNames[block]; ok {
		return names, nil
	}

	if err := p.resolveBlock(block, &hcl.BodySchema{}); err != nil {
		return nil, err
	}

	names := make([]string, 0, len(p.blockValues[block]))
	for i, val := range p.blockValues[block] {
		ectx := p.blockEvalCtx[block][i]

		name := block.Labels[0]
		if err := p.opt.ValidateLabel(name); err != nil {
			return nil, err
		}

		if v, ok := val.Interface().(WithGetName); ok {
			var err error
			name, err = v.GetName(ectx, block, func(expr hcl.Expression) hcl.Diagnostics {
				return p.loadDeps(ectx, expr, nil, true)
			})
			if err != nil {
				return nil, err
			}
			if err := p.opt.ValidateLabel(name); err != nil {
				return nil, err
			}
		}

		setName(val, name)
		names = append(names, name)
	}

	found := map[string]struct{}{}
	for _, name := range names {
		if _, ok := found[name]; ok {
			return nil, errors.Errorf("duplicate name %q", name)
		}
		found[name] = struct{}{}
	}

	p.blockNames[block] = names
	return names, nil
}

func (p *parser) validateVariables(vars map[string]*variable, ectx *hcl.EvalContext) hcl.Diagnostics {
	var diags hcl.Diagnostics
	for _, v := range vars {
		for _, validation := range v.Validations {
			condition, condDiags := validation.Condition.Value(ectx)
			if condDiags.HasErrors() {
				diags = append(diags, condDiags...)
				continue
			}
			if !condition.True() {
				message, msgDiags := validation.ErrorMessage.Value(ectx)
				if msgDiags.HasErrors() {
					diags = append(diags, msgDiags...)
					continue
				}
				diags = append(diags, &hcl.Diagnostic{
					Severity: hcl.DiagError,
					Summary:  "Validation failed",
					Detail:   message.AsString(),
					Subject:  validation.Condition.Range().Ptr(),
				})
			}
		}
	}
	return diags
}

type Variable struct {
	Name        string  `json:"name"`
	Description string  `json:"description,omitempty"`
	Value       *string `json:"value,omitempty"`
}

type ParseMeta struct {
	Renamed      map[string]map[string][]string
	AllVariables []*Variable
}

func Parse(b hcl.Body, opt Opt, val interface{}) (*ParseMeta, hcl.Diagnostics) {
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
		return nil, err
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

		blocks:       map[string]map[string][]*hcl.Block{},
		blockValues:  map[*hcl.Block][]reflect.Value{},
		blockEvalCtx: map[*hcl.Block][]*hcl.EvalContext{},
		blockNames:   map[*hcl.Block][]string{},
		blockTypes:   map[string]reflect.Type{},
		ectx: &hcl.EvalContext{
			Variables: map[string]cty.Value{},
			Functions: Stdlib(),
		},

		progressV: map[uint64]struct{}{},
		progressF: map[uint64]struct{}{},
		progressB: map[uint64]map[string]struct{}{},
		doneB:     map[uint64]map[string]struct{}{},
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
		return nil, diags
	}

	blocks, b, diags := b.PartialContent(defsSchema)
	if diags.HasErrors() {
		return nil, diags
	}

	attrs, diags := b.JustAttributes()
	if diags.HasErrors() {
		if d := removeAttributesDiags(diags, reserved, p.vars, attrs); len(d) > 0 {
			return nil, d
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
		_ = p.resolveValue(p.ectx, k)
	}

	for _, a := range content.Attributes {
		a := a
		return nil, hcl.Diagnostics{
			&hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  "Invalid attribute",
				Detail:   "global attributes currently not supported",
				Subject:  a.Range.Ptr(),
				Context:  a.Range.Ptr(),
			},
		}
	}

	vars := make([]*Variable, 0, len(p.vars))
	for k := range p.vars {
		if err := p.resolveValue(p.ectx, k); err != nil {
			if diags, ok := err.(hcl.Diagnostics); ok {
				return nil, diags
			}
			r := p.vars[k].Body.MissingItemRange()
			return nil, wrapErrorDiagnostic("Invalid value", err, &r, &r)
		}
		v := &Variable{
			Name:        p.vars[k].Name,
			Description: p.vars[k].Description,
		}
		if vv := p.ectx.Variables[k]; !vv.IsNull() {
			var s string
			switch vv.Type() {
			case cty.String:
				s = vv.AsString()
			case cty.Bool:
				s = strconv.FormatBool(vv.True())
			}
			v.Value = &s
		}
		vars = append(vars, v)
	}
	if diags := p.validateVariables(p.vars, p.ectx); diags.HasErrors() {
		return nil, diags
	}

	for k := range p.funcs {
		if err := p.resolveFunction(p.ectx, k); err != nil {
			if diags, ok := err.(hcl.Diagnostics); ok {
				return nil, diags
			}
			var subject *hcl.Range
			var context *hcl.Range
			if p.funcs[k].Params != nil {
				subject = p.funcs[k].Params.Range.Ptr()
				context = subject
			} else {
				for _, block := range blocks.Blocks {
					block := block
					if block.Type == "function" && len(block.Labels) == 1 && block.Labels[0] == k {
						subject = block.LabelRanges[0].Ptr()
						context = block.DefRange.Ptr()
						break
					}
				}
			}
			return nil, wrapErrorDiagnostic("Invalid function", err, subject, context)
		}
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
	renamed := map[string]map[string][]string{}
	vt := reflect.ValueOf(val).Elem().Type()
	for i := 0; i < vt.NumField(); i++ {
		tags := strings.Split(vt.Field(i).Tag.Get("hcl"), ",")

		p.blockTypes[tags[0]] = vt.Field(i).Type.Elem().Elem()
		types[tags[0]] = field{
			idx:    i,
			typ:    vt.Field(i).Type,
			values: make(map[string]value),
		}
		renamed[tags[0]] = map[string][]string{}
	}

	tmpBlocks := map[string]map[string][]*hcl.Block{}
	for _, b := range content.Blocks {
		if len(b.Labels) == 0 || len(b.Labels) > 1 {
			return nil, hcl.Diagnostics{
				&hcl.Diagnostic{
					Severity: hcl.DiagError,
					Summary:  "Invalid block",
					Detail:   fmt.Sprintf("invalid block label: %v", b.Labels),
					Subject:  &b.LabelRanges[0],
					Context:  &b.LabelRanges[0],
				},
			}
		}

		bm, ok := tmpBlocks[b.Type]
		if !ok {
			bm = map[string][]*hcl.Block{}
			tmpBlocks[b.Type] = bm
		}

		names, err := p.resolveBlockNames(b)
		if err != nil {
			return nil, wrapErrorDiagnostic("Invalid name", err, &b.LabelRanges[0], &b.LabelRanges[0])
		}
		for _, name := range names {
			bm[name] = append(bm[name], b)
			renamed[b.Type][b.Labels[0]] = append(renamed[b.Type][b.Labels[0]], name)
		}
	}
	p.blocks = tmpBlocks

	diags = hcl.Diagnostics{}
	for _, b := range content.Blocks {
		b := b
		v := reflect.ValueOf(val)

		err := p.resolveBlock(b, nil)
		if err != nil {
			if diag, ok := err.(hcl.Diagnostics); ok {
				if diag.HasErrors() {
					diags = append(diags, diag...)
					continue
				}
			} else {
				return nil, wrapErrorDiagnostic("Invalid block", err, b.LabelRanges[0].Ptr(), b.DefRange.Ptr())
			}
		}

		vvs := p.blockValues[b]
		for _, vv := range vvs {
			t := types[b.Type]
			lblIndex, lblExists := getNameIndex(vv)
			lblName, _ := getName(vv)
			oldValue, exists := t.values[lblName]
			if !exists && lblExists {
				if v.Elem().Field(t.idx).Type().Kind() == reflect.Slice {
					for i := 0; i < v.Elem().Field(t.idx).Len(); i++ {
						if lblName == v.Elem().Field(t.idx).Index(i).Elem().Field(lblIndex).String() {
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
				t.values[lblName] = value{Value: vv, idx: slice.Len()}
				v.Elem().Field(t.idx).Set(reflect.Append(slice, vv))
			}
		}
	}
	if diags.HasErrors() {
		return nil, diags
	}

	for k := range p.attrs {
		if err := p.resolveValue(p.ectx, k); err != nil {
			if diags, ok := err.(hcl.Diagnostics); ok {
				return nil, diags
			}
			return nil, wrapErrorDiagnostic("Invalid attribute", err, &p.attrs[k].Range, &p.attrs[k].Range)
		}
	}

	return &ParseMeta{
		Renamed:      renamed,
		AllVariables: vars,
	}, nil
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

func setName(v reflect.Value, name string) {
	numFields := v.Elem().Type().NumField()
	for i := 0; i < numFields; i++ {
		parts := strings.Split(v.Elem().Type().Field(i).Tag.Get("hcl"), ",")
		for _, t := range parts[1:] {
			if t == "label" {
				v.Elem().Field(i).Set(reflect.ValueOf(name))
			}
		}
	}
}

func getName(v reflect.Value) (string, bool) {
	numFields := v.Elem().Type().NumField()
	for i := 0; i < numFields; i++ {
		parts := strings.Split(v.Elem().Type().Field(i).Tag.Get("hcl"), ",")
		for _, t := range parts[1:] {
			if t == "label" {
				return v.Elem().Field(i).String(), true
			}
		}
	}
	return "", false
}

func getNameIndex(v reflect.Value) (int, bool) {
	numFields := v.Elem().Type().NumField()
	for i := 0; i < numFields; i++ {
		parts := strings.Split(v.Elem().Type().Field(i).Tag.Get("hcl"), ",")
		for _, t := range parts[1:] {
			if t == "label" {
				return i, true
			}
		}
	}
	return 0, false
}

func removeAttributesDiags(diags hcl.Diagnostics, reserved map[string]struct{}, vars map[string]*variable, attrs hcl.Attributes) hcl.Diagnostics {
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
			for a := range attrs {
				// Do the same for attributes
				if strings.HasPrefix(d.Detail, fmt.Sprintf(`Argument "%s" was already set at `, a)) {
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

// key returns a unique hash for the given values
func key(ks ...any) uint64 {
	hash := fnv.New64a()
	for _, k := range ks {
		v := reflect.ValueOf(k)
		switch v.Kind() {
		case reflect.String:
			hash.Write([]byte(v.String()))
		case reflect.Pointer:
			ptr := reflect.ValueOf(k).Pointer()
			binary.Write(hash, binary.LittleEndian, uint64(ptr))
		default:
			panic(fmt.Sprintf("unknown key kind %s", v.Kind().String()))
		}
	}
	return hash.Sum64()
}

func decodeBody(body hcl.Body, ctx *hcl.EvalContext, val interface{}) hcl.Diagnostics {
	dec := gohcl.DecodeOptions{ImpliedType: ImpliedType}
	return dec.DecodeBody(body, ctx, val)
}
