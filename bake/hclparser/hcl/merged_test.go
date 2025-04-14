// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package hcl

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/davecgh/go-spew/spew"
	"github.com/hashicorp/hcl/v2"
)

func TestMergedBodiesContent(t *testing.T) {
	tests := []struct {
		Bodies    []hcl.Body
		Schema    *hcl.BodySchema
		Want      *hcl.BodyContent
		DiagCount int
	}{
		{
			[]hcl.Body{},
			&hcl.BodySchema{},
			&hcl.BodyContent{
				Attributes: map[string]*hcl.Attribute{},
			},
			0,
		},
		{
			[]hcl.Body{},
			&hcl.BodySchema{
				Attributes: []hcl.AttributeSchema{
					{
						Name: "name",
					},
				},
			},
			&hcl.BodyContent{
				Attributes: map[string]*hcl.Attribute{},
			},
			0,
		},
		{
			[]hcl.Body{},
			&hcl.BodySchema{
				Attributes: []hcl.AttributeSchema{
					{
						Name:     "name",
						Required: true,
					},
				},
			},
			&hcl.BodyContent{
				Attributes: map[string]*hcl.Attribute{},
			},
			1,
		},
		{
			[]hcl.Body{
				&testMergedBodiesVictim{
					HasAttributes: []string{"name"},
				},
			},
			&hcl.BodySchema{
				Attributes: []hcl.AttributeSchema{
					{
						Name: "name",
					},
				},
			},
			&hcl.BodyContent{
				Attributes: map[string]*hcl.Attribute{
					"name": {
						Name: "name",
					},
				},
			},
			0,
		},
		{
			[]hcl.Body{
				&testMergedBodiesVictim{
					Name:          "first",
					HasAttributes: []string{"name"},
				},
				&testMergedBodiesVictim{
					Name:          "second",
					HasAttributes: []string{"name"},
				},
			},
			&hcl.BodySchema{
				Attributes: []hcl.AttributeSchema{
					{
						Name: "name",
					},
				},
			},
			&hcl.BodyContent{
				Attributes: map[string]*hcl.Attribute{
					"name": {
						Name:      "name",
						NameRange: hcl.Range{Filename: "second"},
					},
				},
			},
			1,
		},
		{
			[]hcl.Body{
				&testMergedBodiesVictim{
					Name:          "first",
					HasAttributes: []string{"name"},
				},
				&testMergedBodiesVictim{
					Name:          "second",
					HasAttributes: []string{"age"},
				},
			},
			&hcl.BodySchema{
				Attributes: []hcl.AttributeSchema{
					{
						Name: "name",
					},
					{
						Name: "age",
					},
				},
			},
			&hcl.BodyContent{
				Attributes: map[string]*hcl.Attribute{
					"name": {
						Name:      "name",
						NameRange: hcl.Range{Filename: "first"},
					},
					"age": {
						Name:      "age",
						NameRange: hcl.Range{Filename: "second"},
					},
				},
			},
			0,
		},
		{
			[]hcl.Body{},
			&hcl.BodySchema{
				Blocks: []hcl.BlockHeaderSchema{
					{
						Type: "pizza",
					},
				},
			},
			&hcl.BodyContent{
				Attributes: map[string]*hcl.Attribute{},
			},
			0,
		},
		{
			[]hcl.Body{
				&testMergedBodiesVictim{
					HasBlocks: map[string]int{
						"pizza": 1,
					},
				},
			},
			&hcl.BodySchema{
				Blocks: []hcl.BlockHeaderSchema{
					{
						Type: "pizza",
					},
				},
			},
			&hcl.BodyContent{
				Attributes: map[string]*hcl.Attribute{},
				Blocks: hcl.Blocks{
					{
						Type: "pizza",
					},
				},
			},
			0,
		},
		{
			[]hcl.Body{
				&testMergedBodiesVictim{
					HasBlocks: map[string]int{
						"pizza": 2,
					},
				},
			},
			&hcl.BodySchema{
				Blocks: []hcl.BlockHeaderSchema{
					{
						Type: "pizza",
					},
				},
			},
			&hcl.BodyContent{
				Attributes: map[string]*hcl.Attribute{},
				Blocks: hcl.Blocks{
					{
						Type: "pizza",
					},
					{
						Type: "pizza",
					},
				},
			},
			0,
		},
		{
			[]hcl.Body{
				&testMergedBodiesVictim{
					Name: "first",
					HasBlocks: map[string]int{
						"pizza": 1,
					},
				},
				&testMergedBodiesVictim{
					Name: "second",
					HasBlocks: map[string]int{
						"pizza": 1,
					},
				},
			},
			&hcl.BodySchema{
				Blocks: []hcl.BlockHeaderSchema{
					{
						Type: "pizza",
					},
				},
			},
			&hcl.BodyContent{
				Attributes: map[string]*hcl.Attribute{},
				Blocks: hcl.Blocks{
					{
						Type:     "pizza",
						DefRange: hcl.Range{Filename: "first"},
					},
					{
						Type:     "pizza",
						DefRange: hcl.Range{Filename: "second"},
					},
				},
			},
			0,
		},
		{
			[]hcl.Body{
				&testMergedBodiesVictim{
					Name: "first",
				},
				&testMergedBodiesVictim{
					Name: "second",
					HasBlocks: map[string]int{
						"pizza": 2,
					},
				},
			},
			&hcl.BodySchema{
				Blocks: []hcl.BlockHeaderSchema{
					{
						Type: "pizza",
					},
				},
			},
			&hcl.BodyContent{
				Attributes: map[string]*hcl.Attribute{},
				Blocks: hcl.Blocks{
					{
						Type:     "pizza",
						DefRange: hcl.Range{Filename: "second"},
					},
					{
						Type:     "pizza",
						DefRange: hcl.Range{Filename: "second"},
					},
				},
			},
			0,
		},
		{
			[]hcl.Body{
				&testMergedBodiesVictim{
					Name: "first",
					HasBlocks: map[string]int{
						"pizza": 2,
					},
				},
				&testMergedBodiesVictim{
					Name: "second",
				},
			},
			&hcl.BodySchema{
				Blocks: []hcl.BlockHeaderSchema{
					{
						Type: "pizza",
					},
				},
			},
			&hcl.BodyContent{
				Attributes: map[string]*hcl.Attribute{},
				Blocks: hcl.Blocks{
					{
						Type:     "pizza",
						DefRange: hcl.Range{Filename: "first"},
					},
					{
						Type:     "pizza",
						DefRange: hcl.Range{Filename: "first"},
					},
				},
			},
			0,
		},
		{
			[]hcl.Body{
				&testMergedBodiesVictim{
					Name: "first",
				},
				&testMergedBodiesVictim{
					Name: "second",
				},
			},
			&hcl.BodySchema{
				Blocks: []hcl.BlockHeaderSchema{
					{
						Type: "pizza",
					},
				},
			},
			&hcl.BodyContent{
				Attributes: map[string]*hcl.Attribute{},
			},
			0,
		},
	}

	for i, test := range tests {
		t.Run(fmt.Sprintf("%02d", i), func(t *testing.T) {
			merged := MergeBodies(test.Bodies)
			got, diags := merged.Content(test.Schema)

			if len(diags) != test.DiagCount {
				t.Errorf("Wrong number of diagnostics %d; want %d", len(diags), test.DiagCount)
				for _, diag := range diags {
					t.Logf(" - %s", diag)
				}
			}

			if !reflect.DeepEqual(got, test.Want) {
				t.Errorf("wrong result\ngot:  %s\nwant: %s", spew.Sdump(got), spew.Sdump(test.Want))
			}
		})
	}
}

func TestMergeBodiesPartialContent(t *testing.T) {
	tests := []struct {
		Bodies      []hcl.Body
		Schema      *hcl.BodySchema
		WantContent *hcl.BodyContent
		WantRemain  hcl.Body
		DiagCount   int
	}{
		{
			[]hcl.Body{},
			&hcl.BodySchema{},
			&hcl.BodyContent{
				Attributes: map[string]*hcl.Attribute{},
			},
			mergedBodies{},
			0,
		},
		{
			[]hcl.Body{
				&testMergedBodiesVictim{
					Name:          "first",
					HasAttributes: []string{"name", "age"},
				},
			},
			&hcl.BodySchema{
				Attributes: []hcl.AttributeSchema{
					{
						Name: "name",
					},
				},
			},
			&hcl.BodyContent{
				Attributes: map[string]*hcl.Attribute{
					"name": {
						Name:      "name",
						NameRange: hcl.Range{Filename: "first"},
					},
				},
			},
			mergedBodies{
				&testMergedBodiesVictim{
					Name:          "first",
					HasAttributes: []string{"age"},
				},
			},
			0,
		},
		{
			[]hcl.Body{
				&testMergedBodiesVictim{
					Name:          "first",
					HasAttributes: []string{"name", "age"},
				},
				&testMergedBodiesVictim{
					Name:          "second",
					HasAttributes: []string{"name", "pizza"},
				},
			},
			&hcl.BodySchema{
				Attributes: []hcl.AttributeSchema{
					{
						Name: "name",
					},
				},
			},
			&hcl.BodyContent{
				Attributes: map[string]*hcl.Attribute{
					"name": {
						Name:      "name",
						NameRange: hcl.Range{Filename: "second"},
					},
				},
			},
			mergedBodies{
				&testMergedBodiesVictim{
					Name:          "first",
					HasAttributes: []string{"age"},
				},
				&testMergedBodiesVictim{
					Name:          "second",
					HasAttributes: []string{"pizza"},
				},
			},
			1,
		},
		{
			[]hcl.Body{
				&testMergedBodiesVictim{
					Name:          "first",
					HasAttributes: []string{"name", "age"},
				},
				&testMergedBodiesVictim{
					Name:          "second",
					HasAttributes: []string{"pizza", "soda"},
				},
			},
			&hcl.BodySchema{
				Attributes: []hcl.AttributeSchema{
					{
						Name: "name",
					},
					{
						Name: "soda",
					},
				},
			},
			&hcl.BodyContent{
				Attributes: map[string]*hcl.Attribute{
					"name": {
						Name:      "name",
						NameRange: hcl.Range{Filename: "first"},
					},
					"soda": {
						Name:      "soda",
						NameRange: hcl.Range{Filename: "second"},
					},
				},
			},
			mergedBodies{
				&testMergedBodiesVictim{
					Name:          "first",
					HasAttributes: []string{"age"},
				},
				&testMergedBodiesVictim{
					Name:          "second",
					HasAttributes: []string{"pizza"},
				},
			},
			0,
		},
		{
			[]hcl.Body{
				&testMergedBodiesVictim{
					Name: "first",
					HasBlocks: map[string]int{
						"pizza": 1,
					},
				},
				&testMergedBodiesVictim{
					Name: "second",
					HasBlocks: map[string]int{
						"pizza": 1,
						"soda":  2,
					},
				},
			},
			&hcl.BodySchema{
				Blocks: []hcl.BlockHeaderSchema{
					{
						Type: "pizza",
					},
				},
			},
			&hcl.BodyContent{
				Attributes: map[string]*hcl.Attribute{},
				Blocks: hcl.Blocks{
					{
						Type:     "pizza",
						DefRange: hcl.Range{Filename: "first"},
					},
					{
						Type:     "pizza",
						DefRange: hcl.Range{Filename: "second"},
					},
				},
			},
			mergedBodies{
				&testMergedBodiesVictim{
					Name:          "first",
					HasAttributes: []string{},
					HasBlocks:     map[string]int{},
				},
				&testMergedBodiesVictim{
					Name:          "second",
					HasAttributes: []string{},
					HasBlocks: map[string]int{
						"soda": 2,
					},
				},
			},
			0,
		},
	}

	for i, test := range tests {
		t.Run(fmt.Sprintf("%02d", i), func(t *testing.T) {
			merged := MergeBodies(test.Bodies)
			got, gotRemain, diags := merged.PartialContent(test.Schema)

			if len(diags) != test.DiagCount {
				t.Errorf("Wrong number of diagnostics %d; want %d", len(diags), test.DiagCount)
				for _, diag := range diags {
					t.Logf(" - %s", diag)
				}
			}

			if !reflect.DeepEqual(got, test.WantContent) {
				t.Errorf("wrong content result\ngot:  %s\nwant: %s", spew.Sdump(got), spew.Sdump(test.WantContent))
			}

			if !reflect.DeepEqual(gotRemain, test.WantRemain) {
				t.Errorf("wrong remaining result\ngot:  %s\nwant: %s", spew.Sdump(gotRemain), spew.Sdump(test.WantRemain))
			}
		})
	}
}

type testMergedBodiesVictim struct {
	Name          string
	HasAttributes []string
	HasBlocks     map[string]int
	DiagCount     int
}

func (v *testMergedBodiesVictim) Content(schema *hcl.BodySchema) (*hcl.BodyContent, hcl.Diagnostics) {
	c, _, d := v.PartialContent(schema)
	return c, d
}

func (v *testMergedBodiesVictim) PartialContent(schema *hcl.BodySchema) (*hcl.BodyContent, hcl.Body, hcl.Diagnostics) {
	remain := &testMergedBodiesVictim{
		Name:          v.Name,
		HasAttributes: []string{},
	}

	hasAttrs := map[string]struct{}{}
	for _, n := range v.HasAttributes {
		hasAttrs[n] = struct{}{}

		var found bool
		for _, attrS := range schema.Attributes {
			if n == attrS.Name {
				found = true
				break
			}
		}
		if !found {
			remain.HasAttributes = append(remain.HasAttributes, n)
		}
	}

	content := &hcl.BodyContent{
		Attributes: map[string]*hcl.Attribute{},
	}

	rng := hcl.Range{
		Filename: v.Name,
	}

	for _, attrS := range schema.Attributes {
		_, has := hasAttrs[attrS.Name]
		if has {
			content.Attributes[attrS.Name] = &hcl.Attribute{
				Name:      attrS.Name,
				NameRange: rng,
			}
		}
	}

	if v.HasBlocks != nil {
		for _, blockS := range schema.Blocks {
			num := v.HasBlocks[blockS.Type]
			for range num {
				content.Blocks = append(content.Blocks, &hcl.Block{
					Type:     blockS.Type,
					DefRange: rng,
				})
			}
		}

		remain.HasBlocks = map[string]int{}
		for n := range v.HasBlocks {
			var found bool
			for _, blockS := range schema.Blocks {
				if blockS.Type == n {
					found = true
					break
				}
			}
			if !found {
				remain.HasBlocks[n] = v.HasBlocks[n]
			}
		}
	}

	diags := make(hcl.Diagnostics, v.DiagCount)
	for i := range diags {
		diags[i] = &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  fmt.Sprintf("Fake diagnostic %d", i),
			Detail:   "For testing only.",
			Context:  &rng,
		}
	}

	return content, remain, diags
}

func (v *testMergedBodiesVictim) JustAttributes() (hcl.Attributes, hcl.Diagnostics) {
	attrs := make(map[string]*hcl.Attribute)

	rng := hcl.Range{
		Filename: v.Name,
	}

	for _, name := range v.HasAttributes {
		attrs[name] = &hcl.Attribute{
			Name:      name,
			NameRange: rng,
		}
	}

	diags := make(hcl.Diagnostics, v.DiagCount)
	for i := range diags {
		diags[i] = &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  fmt.Sprintf("Fake diagnostic %d", i),
			Detail:   "For testing only.",
			Context:  &rng,
		}
	}

	return attrs, diags
}

func (v *testMergedBodiesVictim) MissingItemRange() hcl.Range {
	return hcl.Range{
		Filename: v.Name,
	}
}
