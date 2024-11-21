// MIT License
//
// Copyright (c) 2017-2018 Martin Atkins
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package hclparser

import (
	"reflect"

	"github.com/zclconf/go-cty/cty"
)

// ImpliedType takes an arbitrary Go value (as an interface{}) and attempts
// to find a suitable cty.Type instance that could be used for a conversion
// with ToCtyValue.
//
// This allows -- for simple situations at least -- types to be defined just
// once in Go and the cty types derived from the Go types, but in the process
// it makes some assumptions that may be undesirable so applications are
// encouraged to build their cty types directly if exacting control is
// required.
//
// Not all Go types can be represented as cty types, so an error may be
// returned which is usually considered to be a bug in the calling program.
// In particular, ImpliedType will never use capsule types in its returned
// type, because it cannot know the capsule types supported by the calling
// program.
func ImpliedType(gv interface{}) (cty.Type, error) {
	rt := reflect.TypeOf(gv)
	var path cty.Path
	return impliedType(rt, path)
}

func impliedType(rt reflect.Type, path cty.Path) (cty.Type, error) {
	if ety, err := impliedTypeExt(rt, path); err == nil {
		return ety, nil
	}

	switch rt.Kind() {
	case reflect.Ptr:
		return impliedType(rt.Elem(), path)

	// Primitive types
	case reflect.Bool:
		return cty.Bool, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return cty.Number, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return cty.Number, nil
	case reflect.Float32, reflect.Float64:
		return cty.Number, nil
	case reflect.String:
		return cty.String, nil

	// Collection types
	case reflect.Slice:
		path := append(path, cty.IndexStep{Key: cty.UnknownVal(cty.Number)})
		ety, err := impliedType(rt.Elem(), path)
		if err != nil {
			return cty.NilType, err
		}
		return cty.List(ety), nil
	case reflect.Map:
		if !stringType.AssignableTo(rt.Key()) {
			return cty.NilType, path.NewErrorf("no cty.Type for %s (must have string keys)", rt)
		}
		path := append(path, cty.IndexStep{Key: cty.UnknownVal(cty.String)})
		ety, err := impliedType(rt.Elem(), path)
		if err != nil {
			return cty.NilType, err
		}
		return cty.Map(ety), nil

	// Structural types
	case reflect.Struct:
		return impliedStructType(rt, path)

	default:
		return cty.NilType, path.NewErrorf("no cty.Type for %s", rt)
	}
}

func impliedStructType(rt reflect.Type, path cty.Path) (cty.Type, error) {
	if valueType.AssignableTo(rt) {
		// Special case: cty.Value represents cty.DynamicPseudoType, for
		// type conformance checking.
		return cty.DynamicPseudoType, nil
	}

	fieldIdxs := structTagIndices(rt)
	if len(fieldIdxs) == 0 {
		return cty.NilType, path.NewErrorf("no cty.Type for %s (no cty field tags)", rt)
	}

	atys := make(map[string]cty.Type, len(fieldIdxs))

	{
		// Temporary extension of path for attributes
		path := append(path, nil)

		for k, fi := range fieldIdxs {
			path[len(path)-1] = cty.GetAttrStep{Name: k}

			ft := rt.Field(fi).Type
			aty, err := impliedType(ft, path)
			if err != nil {
				return cty.NilType, err
			}

			atys[k] = aty
		}
	}

	return cty.Object(atys), nil
}

var (
	valueType  = reflect.TypeOf(cty.Value{})
	stringType = reflect.TypeOf("")
)

// structTagIndices interrogates the fields of the given type (which must
// be a struct type, or we'll panic) and returns a map from the cty
// attribute names declared via struct tags to the indices of the
// fields holding those tags.
//
// This function will panic if two fields within the struct are tagged with
// the same cty attribute name.
func structTagIndices(st reflect.Type) map[string]int {
	ct := st.NumField()
	ret := make(map[string]int, ct)

	for i := 0; i < ct; i++ {
		field := st.Field(i)
		attrName := field.Tag.Get("cty")
		if attrName != "" {
			ret[attrName] = i
		}
	}

	return ret
}
