// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gohcl

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/davecgh/go-spew/spew"
	"github.com/hashicorp/hcl/v2"
	hclJSON "github.com/hashicorp/hcl/v2/json"
	"github.com/zclconf/go-cty/cty"
)

func TestDecodeBody(t *testing.T) {
	deepEquals := func(other any) func(v any) bool {
		return func(v any) bool {
			return reflect.DeepEqual(v, other)
		}
	}

	type withNameExpression struct {
		Name hcl.Expression `hcl:"name"`
	}

	type withTwoAttributes struct {
		A string `hcl:"a,optional"`
		B string `hcl:"b,optional"`
	}

	type withNestedBlock struct {
		Plain  string             `hcl:"plain,optional"`
		Nested *withTwoAttributes `hcl:"nested,block"`
	}

	type withListofNestedBlocks struct {
		Nested []*withTwoAttributes `hcl:"nested,block"`
	}

	type withListofNestedBlocksNoPointers struct {
		Nested []withTwoAttributes `hcl:"nested,block"`
	}

	tests := []struct {
		Body      map[string]any
		Target    func() any
		Check     func(v any) bool
		DiagCount int
	}{
		{
			map[string]any{},
			makeInstantiateType(struct{}{}),
			deepEquals(struct{}{}),
			0,
		},
		{
			map[string]any{},
			makeInstantiateType(struct {
				Name string `hcl:"name"`
			}{}),
			deepEquals(struct {
				Name string `hcl:"name"`
			}{}),
			1, // name is required
		},
		{
			map[string]any{},
			makeInstantiateType(struct {
				Name *string `hcl:"name"`
			}{}),
			deepEquals(struct {
				Name *string `hcl:"name"`
			}{}),
			0,
		}, // name nil
		{
			map[string]any{},
			makeInstantiateType(struct {
				Name string `hcl:"name,optional"`
			}{}),
			deepEquals(struct {
				Name string `hcl:"name,optional"`
			}{}),
			0,
		}, // name optional
		{
			map[string]any{},
			makeInstantiateType(withNameExpression{}),
			func(v any) bool {
				if v == nil {
					return false
				}

				wne, valid := v.(withNameExpression)
				if !valid {
					return false
				}

				if wne.Name == nil {
					return false
				}

				nameVal, _ := wne.Name.Value(nil)
				return nameVal.IsNull()
			},
			0,
		},
		{
			map[string]any{
				"name": "Ermintrude",
			},
			makeInstantiateType(withNameExpression{}),
			func(v any) bool {
				if v == nil {
					return false
				}

				wne, valid := v.(withNameExpression)
				if !valid {
					return false
				}

				if wne.Name == nil {
					return false
				}

				nameVal, _ := wne.Name.Value(nil)
				return nameVal.Equals(cty.StringVal("Ermintrude")).True()
			},
			0,
		},
		{
			map[string]any{
				"name": "Ermintrude",
			},
			makeInstantiateType(struct {
				Name string `hcl:"name"`
			}{}),
			deepEquals(struct {
				Name string `hcl:"name"`
			}{"Ermintrude"}),
			0,
		},
		{
			map[string]any{
				"name": "Ermintrude",
				"age":  23,
			},
			makeInstantiateType(struct {
				Name string `hcl:"name"`
			}{}),
			deepEquals(struct {
				Name string `hcl:"name"`
			}{"Ermintrude"}),
			1, // Extraneous "age" property
		},
		{
			map[string]any{
				"name": "Ermintrude",
				"age":  50,
			},
			makeInstantiateType(struct {
				Name  string         `hcl:"name"`
				Attrs hcl.Attributes `hcl:",remain"`
			}{}),
			func(gotI any) bool {
				got := gotI.(struct {
					Name  string         `hcl:"name"`
					Attrs hcl.Attributes `hcl:",remain"`
				})
				return got.Name == "Ermintrude" && len(got.Attrs) == 1 && got.Attrs["age"] != nil
			},
			0,
		},
		{
			map[string]any{
				"name": "Ermintrude",
				"age":  50,
			},
			makeInstantiateType(struct {
				Name   string   `hcl:"name"`
				Remain hcl.Body `hcl:",remain"`
			}{}),
			func(gotI any) bool {
				got := gotI.(struct {
					Name   string   `hcl:"name"`
					Remain hcl.Body `hcl:",remain"`
				})

				attrs, _ := got.Remain.JustAttributes()

				return got.Name == "Ermintrude" && len(attrs) == 1 && attrs["age"] != nil
			},
			0,
		},
		{
			map[string]any{
				"name":   "Ermintrude",
				"living": true,
			},
			makeInstantiateType(struct {
				Name   string               `hcl:"name"`
				Remain map[string]cty.Value `hcl:",remain"`
			}{}),
			deepEquals(struct {
				Name   string               `hcl:"name"`
				Remain map[string]cty.Value `hcl:",remain"`
			}{
				Name: "Ermintrude",
				Remain: map[string]cty.Value{
					"living": cty.True,
				},
			}),
			0,
		},
		{
			map[string]any{
				"name": "Ermintrude",
				"age":  50,
			},
			makeInstantiateType(struct {
				Name   string   `hcl:"name"`
				Body   hcl.Body `hcl:",body"`
				Remain hcl.Body `hcl:",remain"`
			}{}),
			func(gotI any) bool {
				got := gotI.(struct {
					Name   string   `hcl:"name"`
					Body   hcl.Body `hcl:",body"`
					Remain hcl.Body `hcl:",remain"`
				})

				attrs, _ := got.Body.JustAttributes()

				return got.Name == "Ermintrude" && len(attrs) == 2 &&
					attrs["name"] != nil && attrs["age"] != nil
			},
			0,
		},
		{
			map[string]any{
				"noodle": map[string]any{},
			},
			makeInstantiateType(struct {
				Noodle struct{} `hcl:"noodle,block"`
			}{}),
			func(gotI any) bool {
				// Generating no diagnostics is good enough for this one.
				return true
			},
			0,
		},
		{
			map[string]any{
				"noodle": []map[string]any{{}},
			},
			makeInstantiateType(struct {
				Noodle struct{} `hcl:"noodle,block"`
			}{}),
			func(gotI any) bool {
				// Generating no diagnostics is good enough for this one.
				return true
			},
			0,
		},
		{
			map[string]any{
				"noodle": []map[string]any{{}, {}},
			},
			makeInstantiateType(struct {
				Noodle struct{} `hcl:"noodle,block"`
			}{}),
			func(gotI any) bool {
				// Generating one diagnostic is good enough for this one.
				return true
			},
			1,
		},
		{
			map[string]any{},
			makeInstantiateType(struct {
				Noodle struct{} `hcl:"noodle,block"`
			}{}),
			func(gotI any) bool {
				// Generating one diagnostic is good enough for this one.
				return true
			},
			1,
		},
		{
			map[string]any{
				"noodle": []map[string]any{},
			},
			makeInstantiateType(struct {
				Noodle struct{} `hcl:"noodle,block"`
			}{}),
			func(gotI any) bool {
				// Generating one diagnostic is good enough for this one.
				return true
			},
			1,
		},
		{
			map[string]any{
				"noodle": map[string]any{},
			},
			makeInstantiateType(struct {
				Noodle *struct{} `hcl:"noodle,block"`
			}{}),
			func(gotI any) bool {
				return gotI.(struct {
					Noodle *struct{} `hcl:"noodle,block"`
				}).Noodle != nil
			},
			0,
		},
		{
			map[string]any{
				"noodle": []map[string]any{{}},
			},
			makeInstantiateType(struct {
				Noodle *struct{} `hcl:"noodle,block"`
			}{}),
			func(gotI any) bool {
				return gotI.(struct {
					Noodle *struct{} `hcl:"noodle,block"`
				}).Noodle != nil
			},
			0,
		},
		{
			map[string]any{
				"noodle": []map[string]any{},
			},
			makeInstantiateType(struct {
				Noodle *struct{} `hcl:"noodle,block"`
			}{}),
			func(gotI any) bool {
				return gotI.(struct {
					Noodle *struct{} `hcl:"noodle,block"`
				}).Noodle == nil
			},
			0,
		},
		{
			map[string]any{
				"noodle": []map[string]any{{}, {}},
			},
			makeInstantiateType(struct {
				Noodle *struct{} `hcl:"noodle,block"`
			}{}),
			func(gotI any) bool {
				// Generating one diagnostic is good enough for this one.
				return true
			},
			1,
		},
		{
			map[string]any{
				"noodle": []map[string]any{},
			},
			makeInstantiateType(struct {
				Noodle []struct{} `hcl:"noodle,block"`
			}{}),
			func(gotI any) bool {
				noodle := gotI.(struct {
					Noodle []struct{} `hcl:"noodle,block"`
				}).Noodle
				return len(noodle) == 0
			},
			0,
		},
		{
			map[string]any{
				"noodle": []map[string]any{{}},
			},
			makeInstantiateType(struct {
				Noodle []struct{} `hcl:"noodle,block"`
			}{}),
			func(gotI any) bool {
				noodle := gotI.(struct {
					Noodle []struct{} `hcl:"noodle,block"`
				}).Noodle
				return len(noodle) == 1
			},
			0,
		},
		{
			map[string]any{
				"noodle": []map[string]any{{}, {}},
			},
			makeInstantiateType(struct {
				Noodle []struct{} `hcl:"noodle,block"`
			}{}),
			func(gotI any) bool {
				noodle := gotI.(struct {
					Noodle []struct{} `hcl:"noodle,block"`
				}).Noodle
				return len(noodle) == 2
			},
			0,
		},
		{
			map[string]any{
				"noodle": map[string]any{},
			},
			makeInstantiateType(struct {
				Noodle struct {
					Name string `hcl:"name,label"`
				} `hcl:"noodle,block"`
			}{}),
			func(gotI any) bool {
				//nolint:misspell
				// Generating two diagnostics is good enough for this one.
				// (one for the missing noodle block and the other for
				// the JSON serialization detecting the missing level of
				// heirarchy for the label.)
				return true
			},
			2,
		},
		{
			map[string]any{
				"noodle": map[string]any{
					"foo_foo": map[string]any{},
				},
			},
			makeInstantiateType(struct {
				Noodle struct {
					Name string `hcl:"name,label"`
				} `hcl:"noodle,block"`
			}{}),
			func(gotI any) bool {
				noodle := gotI.(struct {
					Noodle struct {
						Name string `hcl:"name,label"`
					} `hcl:"noodle,block"`
				}).Noodle
				return noodle.Name == "foo_foo"
			},
			0,
		},
		{
			map[string]any{
				"noodle": map[string]any{
					"foo_foo": map[string]any{},
					"bar_baz": map[string]any{},
				},
			},
			makeInstantiateType(struct {
				Noodle struct {
					Name string `hcl:"name,label"`
				} `hcl:"noodle,block"`
			}{}),
			func(gotI any) bool {
				// One diagnostic is enough for this one.
				return true
			},
			1,
		},
		{
			map[string]any{
				"noodle": map[string]any{
					"foo_foo": map[string]any{},
					"bar_baz": map[string]any{},
				},
			},
			makeInstantiateType(struct {
				Noodles []struct {
					Name string `hcl:"name,label"`
				} `hcl:"noodle,block"`
			}{}),
			func(gotI any) bool {
				noodles := gotI.(struct {
					Noodles []struct {
						Name string `hcl:"name,label"`
					} `hcl:"noodle,block"`
				}).Noodles
				return len(noodles) == 2 && (noodles[0].Name == "foo_foo" || noodles[0].Name == "bar_baz") && (noodles[1].Name == "foo_foo" || noodles[1].Name == "bar_baz") && noodles[0].Name != noodles[1].Name
			},
			0,
		},
		{
			map[string]any{
				"noodle": map[string]any{
					"foo_foo": map[string]any{
						"type": "rice",
					},
				},
			},
			makeInstantiateType(struct {
				Noodle struct {
					Name string `hcl:"name,label"`
					Type string `hcl:"type"`
				} `hcl:"noodle,block"`
			}{}),
			func(gotI any) bool {
				noodle := gotI.(struct {
					Noodle struct {
						Name string `hcl:"name,label"`
						Type string `hcl:"type"`
					} `hcl:"noodle,block"`
				}).Noodle
				return noodle.Name == "foo_foo" && noodle.Type == "rice"
			},
			0,
		},

		{
			map[string]any{
				"name": "Ermintrude",
				"age":  34,
			},
			makeInstantiateType(map[string]string(nil)),
			deepEquals(map[string]string{
				"name": "Ermintrude",
				"age":  "34",
			}),
			0,
		},
		{
			map[string]any{
				"name": "Ermintrude",
				"age":  89,
			},
			makeInstantiateType(map[string]*hcl.Attribute(nil)),
			func(gotI any) bool {
				got := gotI.(map[string]*hcl.Attribute)
				return len(got) == 2 && got["name"] != nil && got["age"] != nil
			},
			0,
		},
		{
			map[string]any{
				"name": "Ermintrude",
				"age":  13,
			},
			makeInstantiateType(map[string]hcl.Expression(nil)),
			func(gotI any) bool {
				got := gotI.(map[string]hcl.Expression)
				return len(got) == 2 && got["name"] != nil && got["age"] != nil
			},
			0,
		},
		{
			map[string]any{
				"name":   "Ermintrude",
				"living": true,
			},
			makeInstantiateType(map[string]cty.Value(nil)),
			deepEquals(map[string]cty.Value{
				"name":   cty.StringVal("Ermintrude"),
				"living": cty.True,
			}),
			0,
		},
		{
			// Retain "nested" block while decoding
			map[string]any{
				"plain": "foo",
			},
			func() any {
				return &withNestedBlock{
					Plain: "bar",
					Nested: &withTwoAttributes{
						A: "bar",
					},
				}
			},
			func(gotI any) bool {
				foo := gotI.(withNestedBlock)
				return foo.Plain == "foo" && foo.Nested != nil && foo.Nested.A == "bar"
			},
			0,
		},
		{
			// Retain values in "nested" block while decoding
			map[string]any{
				"nested": map[string]any{
					"a": "foo",
				},
			},
			func() any {
				return &withNestedBlock{
					Nested: &withTwoAttributes{
						B: "bar",
					},
				}
			},
			func(gotI any) bool {
				foo := gotI.(withNestedBlock)
				return foo.Nested.A == "foo" && foo.Nested.B == "bar"
			},
			0,
		},
		{
			// Retain values in "nested" block list while decoding
			map[string]any{
				"nested": []map[string]any{
					{
						"a": "foo",
					},
				},
			},
			func() any {
				return &withListofNestedBlocks{
					Nested: []*withTwoAttributes{
						{
							B: "bar",
						},
					},
				}
			},
			func(gotI any) bool {
				n := gotI.(withListofNestedBlocks)
				return n.Nested[0].A == "foo" && n.Nested[0].B == "bar"
			},
			0,
		},
		{
			// Remove additional elements from the list while decoding nested blocks
			map[string]any{
				"nested": []map[string]any{
					{
						"a": "foo",
					},
				},
			},
			func() any {
				return &withListofNestedBlocks{
					Nested: []*withTwoAttributes{
						{
							B: "bar",
						},
						{
							B: "bar",
						},
					},
				}
			},
			func(gotI any) bool {
				n := gotI.(withListofNestedBlocks)
				return len(n.Nested) == 1
			},
			0,
		},
		{
			// Make sure decoding value slices works the same as pointer slices.
			map[string]any{
				"nested": []map[string]any{
					{
						"b": "bar",
					},
					{
						"b": "baz",
					},
				},
			},
			func() any {
				return &withListofNestedBlocksNoPointers{
					Nested: []withTwoAttributes{
						{
							B: "foo",
						},
					},
				}
			},
			func(gotI any) bool {
				n := gotI.(withListofNestedBlocksNoPointers)
				return n.Nested[0].B == "bar" && len(n.Nested) == 2
			},
			0,
		},
	}

	for i, test := range tests {
		// For convenience here we're going to use the JSON parser
		// to process the given body.
		buf, err := json.Marshal(test.Body)
		if err != nil {
			t.Fatalf("error JSON-encoding body for test %d: %s", i, err)
		}

		t.Run(string(buf), func(t *testing.T) {
			file, diags := hclJSON.Parse(buf, "test.json")
			if len(diags) != 0 {
				t.Fatalf("diagnostics while parsing: %s", diags.Error())
			}

			targetVal := reflect.ValueOf(test.Target())

			diags = DecodeBody(file.Body, nil, targetVal.Interface())
			if len(diags) != test.DiagCount {
				t.Errorf("wrong number of diagnostics %d; want %d", len(diags), test.DiagCount)
				for _, diag := range diags {
					t.Logf(" - %s", diag.Error())
				}
			}
			got := targetVal.Elem().Interface()
			if !test.Check(got) {
				t.Errorf("wrong result\ngot:  %s", spew.Sdump(got))
			}
		})
	}
}

func TestDecodeExpression(t *testing.T) {
	tests := []struct {
		Value     cty.Value
		Target    any
		Want      any
		DiagCount int
	}{
		{
			cty.StringVal("hello"),
			"",
			"hello",
			0,
		},
		{
			cty.StringVal("hello"),
			cty.NilVal,
			cty.StringVal("hello"),
			0,
		},
		{
			cty.NumberIntVal(2),
			"",
			"2",
			0,
		},
		{
			cty.StringVal("true"),
			false,
			true,
			0,
		},
		{
			cty.NullVal(cty.String),
			"",
			"",
			1, // null value is not allowed
		},
		{
			cty.UnknownVal(cty.String),
			"",
			"",
			1, // value must be known
		},
		{
			cty.ListVal([]cty.Value{cty.True}),
			false,
			false,
			1, // bool required
		},
	}

	for i, test := range tests {
		t.Run(fmt.Sprintf("%02d", i), func(t *testing.T) {
			expr := &fixedExpression{test.Value}

			targetVal := reflect.New(reflect.TypeOf(test.Target))

			diags := DecodeExpression(expr, nil, targetVal.Interface())
			if len(diags) != test.DiagCount {
				t.Errorf("wrong number of diagnostics %d; want %d", len(diags), test.DiagCount)
				for _, diag := range diags {
					t.Logf(" - %s", diag.Error())
				}
			}
			got := targetVal.Elem().Interface()
			if !reflect.DeepEqual(got, test.Want) {
				t.Errorf("wrong result\ngot:  %#v\nwant: %#v", got, test.Want)
			}
		})
	}
}

type fixedExpression struct {
	val cty.Value
}

func (e *fixedExpression) Value(ctx *hcl.EvalContext) (cty.Value, hcl.Diagnostics) {
	return e.val, nil
}

func (e *fixedExpression) Range() (r hcl.Range) {
	return
}

func (e *fixedExpression) StartRange() (r hcl.Range) {
	return
}

func (e *fixedExpression) Variables() []hcl.Traversal {
	return nil
}

func makeInstantiateType(target any) func() any {
	return func() any {
		return reflect.New(reflect.TypeOf(target)).Interface()
	}
}
