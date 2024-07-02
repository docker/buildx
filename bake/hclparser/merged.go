// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

// Forked from https://github.com/hashicorp/hcl/blob/4679383728fe331fc8a6b46036a27b8f818d9bc0/merged.go

package hclparser

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
)

// MergeFiles combines the given files to produce a single body that contains
// configuration from all of the given files.
//
// The ordering of the given files decides the order in which contained
// elements will be returned. If any top-level attributes are defined with
// the same name across multiple files, a diagnostic will be produced from
// the Content and PartialContent methods describing this error in a
// user-friendly way.
func MergeFiles(files []*hcl.File) hcl.Body {
	var bodies []hcl.Body
	for _, file := range files {
		bodies = append(bodies, file.Body)
	}
	return MergeBodies(bodies)
}

// MergeBodies is like MergeFiles except it deals directly with bodies, rather
// than with entire files.
func MergeBodies(bodies []hcl.Body) hcl.Body {
	if len(bodies) == 0 {
		// Swap out for our singleton empty body, to reduce the number of
		// empty slices we have hanging around.
		return emptyBody
	}

	// If any of the given bodies are already merged bodies, we'll unpack
	// to flatten to a single mergedBodies, since that's conceptually simpler.
	// This also, as a side-effect, eliminates any empty bodies, since
	// empties are merged bodies with no inner bodies.
	var newLen int
	var flatten bool
	for _, body := range bodies {
		if children, merged := body.(mergedBodies); merged {
			newLen += len(children)
			flatten = true
		} else {
			newLen++
		}
	}

	if !flatten { // not just newLen == len, because we might have mergedBodies with single bodies inside
		return mergedBodies(bodies)
	}

	if newLen == 0 {
		// Don't allocate a new empty when we already have one
		return emptyBody
	}

	n := make([]hcl.Body, 0, newLen)
	for _, body := range bodies {
		if children, merged := body.(mergedBodies); merged {
			n = append(n, children...)
		} else {
			n = append(n, body)
		}
	}
	return mergedBodies(n)
}

var emptyBody = mergedBodies([]hcl.Body{})

// EmptyBody returns a body with no content. This body can be used as a
// placeholder when a body is required but no body content is available.
func EmptyBody() hcl.Body {
	return emptyBody
}

type mergedBodies []hcl.Body

// Content returns the content produced by applying the given schema to all
// of the merged bodies and merging the result.
//
// Although required attributes _are_ supported, they should be used sparingly
// with merged bodies since in this case there is no contextual information
// with which to return good diagnostics. Applications working with merged
// bodies may wish to mark all attributes as optional and then check for
// required attributes afterwards, to produce better diagnostics.
func (mb mergedBodies) Content(schema *hcl.BodySchema) (*hcl.BodyContent, hcl.Diagnostics) {
	// the returned body will always be empty in this case, because mergedContent
	// will only ever call Content on the child bodies.
	content, _, diags := mb.mergedContent(schema, false)
	return content, diags
}

func (mb mergedBodies) PartialContent(schema *hcl.BodySchema) (*hcl.BodyContent, hcl.Body, hcl.Diagnostics) {
	return mb.mergedContent(schema, true)
}

func (mb mergedBodies) JustAttributes() (hcl.Attributes, hcl.Diagnostics) {
	attrs := make(map[string]*hcl.Attribute)
	var diags hcl.Diagnostics

	for _, body := range mb {
		thisAttrs, thisDiags := body.JustAttributes()

		if len(thisDiags) != 0 {
			diags = append(diags, thisDiags...)
		}

		for name, attr := range thisAttrs {
			if existing := attrs[name]; existing != nil {
				diags = diags.Append(&hcl.Diagnostic{
					Severity: hcl.DiagError,
					Summary:  "Duplicate argument",
					Detail: fmt.Sprintf(
						"Argument %q was already set at %s",
						name, existing.NameRange.String(),
					),
					Subject: thisAttrs[name].NameRange.Ptr(),
				})
			}
			attrs[name] = attr
		}
	}

	return attrs, diags
}

func (mb mergedBodies) MissingItemRange() hcl.Range {
	if len(mb) == 0 {
		// Nothing useful to return here, so we'll return some garbage.
		return hcl.Range{
			Filename: "<empty>",
		}
	}

	// arbitrarily use the first body's missing item range
	return mb[0].MissingItemRange()
}

func (mb mergedBodies) mergedContent(schema *hcl.BodySchema, partial bool) (*hcl.BodyContent, hcl.Body, hcl.Diagnostics) {
	// We need to produce a new schema with none of the attributes marked as
	// required, since _any one_ of our bodies can contribute an attribute value.
	// We'll separately check that all required attributes are present at
	// the end.
	mergedSchema := &hcl.BodySchema{
		Blocks: schema.Blocks,
	}
	for _, attrS := range schema.Attributes {
		mergedAttrS := attrS
		mergedAttrS.Required = false
		mergedSchema.Attributes = append(mergedSchema.Attributes, mergedAttrS)
	}

	var mergedLeftovers []hcl.Body
	content := &hcl.BodyContent{
		Attributes: map[string]*hcl.Attribute{},
	}

	var diags hcl.Diagnostics
	for _, body := range mb {
		var thisContent *hcl.BodyContent
		var thisLeftovers hcl.Body
		var thisDiags hcl.Diagnostics

		if partial {
			thisContent, thisLeftovers, thisDiags = body.PartialContent(mergedSchema)
		} else {
			thisContent, thisDiags = body.Content(mergedSchema)
		}

		if thisLeftovers != nil {
			mergedLeftovers = append(mergedLeftovers, thisLeftovers)
		}
		if len(thisDiags) != 0 {
			diags = append(diags, thisDiags...)
		}

		if thisContent.Attributes != nil {
			for name, attr := range thisContent.Attributes {
				if existing := content.Attributes[name]; existing != nil {
					diags = diags.Append(&hcl.Diagnostic{
						Severity: hcl.DiagError,
						Summary:  "Duplicate argument",
						Detail: fmt.Sprintf(
							"Argument %q was already set at %s",
							name, existing.NameRange.String(),
						),
						Subject: thisContent.Attributes[name].NameRange.Ptr(),
					})
				}
				content.Attributes[name] = attr
			}
		}

		if len(thisContent.Blocks) != 0 {
			content.Blocks = append(content.Blocks, thisContent.Blocks...)
		}
	}

	// Finally, we check for required attributes.
	for _, attrS := range schema.Attributes {
		if !attrS.Required {
			continue
		}

		if content.Attributes[attrS.Name] == nil {
			// We don't have any context here to produce a good diagnostic,
			// which is why we warn in the Content docstring to minimize the
			// use of required attributes on merged bodies.
			diags = diags.Append(&hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  "Missing required argument",
				Detail: fmt.Sprintf(
					"The argument %q is required, but was not set.",
					attrS.Name,
				),
			})
		}
	}

	leftoverBody := MergeBodies(mergedLeftovers)
	return content, leftoverBody, diags
}
