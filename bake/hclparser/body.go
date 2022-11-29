package hclparser

import (
	"github.com/hashicorp/hcl/v2"
)

type filterBody struct {
	body    hcl.Body
	schema  *hcl.BodySchema
	exclude bool
}

func FilterIncludeBody(body hcl.Body, schema *hcl.BodySchema) hcl.Body {
	return &filterBody{
		body:   body,
		schema: schema,
	}
}

func FilterExcludeBody(body hcl.Body, schema *hcl.BodySchema) hcl.Body {
	return &filterBody{
		body:    body,
		schema:  schema,
		exclude: true,
	}
}

func (b *filterBody) Content(schema *hcl.BodySchema) (*hcl.BodyContent, hcl.Diagnostics) {
	if b.exclude {
		schema = subtractSchemas(schema, b.schema)
	} else {
		schema = intersectSchemas(schema, b.schema)
	}
	content, _, diag := b.body.PartialContent(schema)
	return content, diag
}

func (b *filterBody) PartialContent(schema *hcl.BodySchema) (*hcl.BodyContent, hcl.Body, hcl.Diagnostics) {
	if b.exclude {
		schema = subtractSchemas(schema, b.schema)
	} else {
		schema = intersectSchemas(schema, b.schema)
	}
	return b.body.PartialContent(schema)
}

func (b *filterBody) JustAttributes() (hcl.Attributes, hcl.Diagnostics) {
	return b.body.JustAttributes()
}

func (b *filterBody) MissingItemRange() hcl.Range {
	return b.body.MissingItemRange()
}

func intersectSchemas(a, b *hcl.BodySchema) *hcl.BodySchema {
	result := &hcl.BodySchema{}
	for _, blockA := range a.Blocks {
		for _, blockB := range b.Blocks {
			if blockA.Type == blockB.Type {
				result.Blocks = append(result.Blocks, blockA)
				break
			}
		}
	}
	for _, attrA := range a.Attributes {
		for _, attrB := range b.Attributes {
			if attrA.Name == attrB.Name {
				result.Attributes = append(result.Attributes, attrA)
				break
			}
		}
	}
	return result
}

func subtractSchemas(a, b *hcl.BodySchema) *hcl.BodySchema {
	result := &hcl.BodySchema{}
	for _, blockA := range a.Blocks {
		found := false
		for _, blockB := range b.Blocks {
			if blockA.Type == blockB.Type {
				found = true
				break
			}
		}
		if !found {
			result.Blocks = append(result.Blocks, blockA)
		}
	}
	for _, attrA := range a.Attributes {
		found := false
		for _, attrB := range b.Attributes {
			if attrA.Name == attrB.Name {
				found = true
				break
			}
		}
		if !found {
			result.Attributes = append(result.Attributes, attrA)
		}
	}
	return result
}
