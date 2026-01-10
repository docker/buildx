// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gohcl

import (
	"reflect"

	"github.com/hashicorp/hcl/v2"
)

var exprType = reflect.TypeFor[hcl.Expression]()
var bodyType = reflect.TypeFor[hcl.Body]()
var blockType = reflect.TypeFor[*hcl.Block]() //nolint:unused
var attrType = reflect.TypeFor[*hcl.Attribute]()
var attrsType = reflect.TypeFor[hcl.Attributes]()
