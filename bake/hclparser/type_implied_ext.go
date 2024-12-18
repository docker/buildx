package hclparser

import (
	"reflect"
	"sync"

	"github.com/containerd/errdefs"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/convert"
	"github.com/zclconf/go-cty/cty/gocty"
)

type ToCtyValueConverter interface {
	// ToCtyValue will convert this capsule value into a native
	// cty.Value. This should not return a capsule type.
	ToCtyValue() cty.Value
}

type FromCtyValueConverter interface {
	// FromCtyValue will initialize this value using a cty.Value.
	FromCtyValue(in cty.Value, path cty.Path) error
}

type extensionType int

const (
	unwrapCapsuleValueExtension extensionType = iota
)

func impliedTypeExt(rt reflect.Type, _ cty.Path) (cty.Type, error) {
	if rt.Kind() != reflect.Pointer {
		rt = reflect.PointerTo(rt)
	}

	if isCapsuleType(rt) {
		return capsuleValueCapsuleType(rt), nil
	}
	return cty.NilType, errdefs.ErrNotImplemented
}

func isCapsuleType(rt reflect.Type) bool {
	fromCtyValueType := reflect.TypeFor[FromCtyValueConverter]()
	toCtyValueType := reflect.TypeFor[ToCtyValueConverter]()
	return rt.Implements(fromCtyValueType) && rt.Implements(toCtyValueType)
}

var capsuleValueTypes sync.Map

func capsuleValueCapsuleType(rt reflect.Type) cty.Type {
	if rt.Kind() != reflect.Pointer {
		panic("capsule value must be a pointer")
	}

	elem := rt.Elem()
	if val, loaded := capsuleValueTypes.Load(elem); loaded {
		return val.(cty.Type)
	}

	toCtyValueType := reflect.TypeFor[ToCtyValueConverter]()

	// First time used. Initialize new capsule ops.
	ops := &cty.CapsuleOps{
		ConversionTo: func(_ cty.Type) func(cty.Value, cty.Path) (any, error) {
			return func(in cty.Value, p cty.Path) (any, error) {
				rv := reflect.New(elem).Interface()
				if err := rv.(FromCtyValueConverter).FromCtyValue(in, p); err != nil {
					return nil, err
				}
				return rv, nil
			}
		},
		ConversionFrom: func(want cty.Type) func(any, cty.Path) (cty.Value, error) {
			return func(in any, _ cty.Path) (cty.Value, error) {
				rv := reflect.ValueOf(in).Convert(toCtyValueType)
				v := rv.Interface().(ToCtyValueConverter).ToCtyValue()
				return convert.Convert(v, want)
			}
		},
		ExtensionData: func(key any) any {
			switch key {
			case unwrapCapsuleValueExtension:
				zero := reflect.Zero(elem).Interface()
				if conv, ok := zero.(ToCtyValueConverter); ok {
					return conv.ToCtyValue().Type()
				}

				zero = reflect.Zero(rt).Interface()
				if conv, ok := zero.(ToCtyValueConverter); ok {
					return conv.ToCtyValue().Type()
				}
			}
			return nil
		},
	}

	// Attempt to store the new type. Use whichever was loaded first in the case
	// of a race condition.
	ety := cty.CapsuleWithOps(elem.Name(), elem, ops)
	val, _ := capsuleValueTypes.LoadOrStore(elem, ety)
	return val.(cty.Type)
}

// UnwrapCtyValue will unwrap capsule type values into their native cty value
// equivalents if possible.
func UnwrapCtyValue(in cty.Value) cty.Value {
	want := toCtyValueType(in.Type())
	if in.Type().Equals(want) {
		return in
	} else if out, err := convert.Convert(in, want); err == nil {
		return out
	}
	return cty.NullVal(want)
}

func toCtyValueType(in cty.Type) cty.Type {
	if et := in.MapElementType(); et != nil {
		return cty.Map(toCtyValueType(*et))
	}

	if et := in.SetElementType(); et != nil {
		return cty.Set(toCtyValueType(*et))
	}

	if et := in.ListElementType(); et != nil {
		return cty.List(toCtyValueType(*et))
	}

	if in.IsObjectType() {
		var optional []string
		inAttrTypes := in.AttributeTypes()
		outAttrTypes := make(map[string]cty.Type, len(inAttrTypes))
		for name, typ := range inAttrTypes {
			outAttrTypes[name] = toCtyValueType(typ)
			if in.AttributeOptional(name) {
				optional = append(optional, name)
			}
		}
		return cty.ObjectWithOptionalAttrs(outAttrTypes, optional)
	}

	if in.IsTupleType() {
		inTypes := in.TupleElementTypes()
		outTypes := make([]cty.Type, len(inTypes))
		for i, typ := range inTypes {
			outTypes[i] = toCtyValueType(typ)
		}
		return cty.Tuple(outTypes)
	}

	if in.IsCapsuleType() {
		if out := in.CapsuleExtensionData(unwrapCapsuleValueExtension); out != nil {
			return out.(cty.Type)
		}
		return cty.DynamicPseudoType
	}

	return in
}

func ToCtyValue(val any, ty cty.Type) (cty.Value, error) {
	out, err := gocty.ToCtyValue(val, ty)
	if err != nil {
		return out, err
	}
	return UnwrapCtyValue(out), nil
}
