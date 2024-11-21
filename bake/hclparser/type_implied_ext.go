package hclparser

import (
	"reflect"
	"sync"

	"github.com/containerd/errdefs"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/convert"
	"github.com/zclconf/go-cty/cty/gocty"
)

type CapsuleValue interface {
	// FromCtyValue will initialize this value using a cty.Value.
	FromCtyValue(in cty.Value, path cty.Path) error

	// ToCtyValue will convert this capsule value into a native
	// cty.Value. This should not return a capsule type.
	ToCtyValue() cty.Value
}

type extensionType int

const (
	nativeTypeExtension extensionType = iota
)

func impliedTypeExt(rt reflect.Type, _ cty.Path) (cty.Type, error) {
	if rt.AssignableTo(capsuleValueType) {
		return capsuleValueCapsuleType(rt), nil
	}
	return cty.NilType, errdefs.ErrNotImplemented
}

var (
	capsuleValueType  = reflect.TypeFor[CapsuleValue]()
	capsuleValueTypes sync.Map
)

func capsuleValueCapsuleType(rt reflect.Type) cty.Type {
	if val, loaded := capsuleValueTypes.Load(rt); loaded {
		return val.(cty.Type)
	}

	// First time used.
	ety := cty.CapsuleWithOps(rt.Name(), rt.Elem(), &cty.CapsuleOps{
		ConversionTo: func(_ cty.Type) func(cty.Value, cty.Path) (any, error) {
			return func(in cty.Value, p cty.Path) (any, error) {
				rv := reflect.New(rt.Elem()).Interface()
				if err := rv.(CapsuleValue).FromCtyValue(in, p); err != nil {
					return nil, err
				}
				return rv, nil
			}
		},
		ConversionFrom: func(want cty.Type) func(any, cty.Path) (cty.Value, error) {
			return func(in any, _ cty.Path) (cty.Value, error) {
				v := in.(CapsuleValue).ToCtyValue()
				return convert.Convert(v, want)
			}
		},
		ExtensionData: func(key any) any {
			switch key {
			case nativeTypeExtension:
				zero := reflect.Zero(rt).Interface()
				return zero.(CapsuleValue).ToCtyValue().Type()
			default:
				return nil
			}
		},
	})

	// Attempt to store the new type. Use whichever was loaded first in the case of a race condition.
	val, _ := capsuleValueTypes.LoadOrStore(rt, ety)
	return val.(cty.Type)
}

// ToNativeValue will convert a value to only native cty types which will
// remove capsule types if possible.
func ToNativeValue(in cty.Value) cty.Value {
	want := toNativeType(in.Type())
	if in.Type().Equals(want) {
		return in
	} else if out, err := convert.Convert(in, want); err == nil {
		return out
	}
	return cty.NullVal(want)
}

func toNativeType(in cty.Type) cty.Type {
	if et := in.MapElementType(); et != nil {
		return cty.Map(toNativeType(*et))
	}

	if et := in.SetElementType(); et != nil {
		return cty.Set(toNativeType(*et))
	}

	if et := in.ListElementType(); et != nil {
		return cty.List(toNativeType(*et))
	}

	if in.IsObjectType() {
		var optional []string
		inAttrTypes := in.AttributeTypes()
		outAttrTypes := make(map[string]cty.Type, len(inAttrTypes))
		for name, typ := range inAttrTypes {
			outAttrTypes[name] = toNativeType(typ)
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
			outTypes[i] = toNativeType(typ)
		}
		return cty.Tuple(outTypes)
	}

	if in.IsCapsuleType() {
		if out := in.CapsuleExtensionData(nativeTypeExtension); out != nil {
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
	return ToNativeValue(out), nil
}
