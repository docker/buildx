package hclparser

import (
	"reflect"
	"unsafe"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/pkg/errors"
)

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
