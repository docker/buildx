package buildflags

import (
	"strconv"

	"github.com/zclconf/go-cty/cty"
)

func (p *PullPolicy) FromCtyValue(in cty.Value, path cty.Path) error {
	var value string
	switch in.Type() {
	case cty.String:
		value = in.AsString()
	case cty.Bool:
		value = strconv.FormatBool(in.True())
	default:
		return path.NewErrorf("pull must be a boolean or a string")
	}
	if err := p.Set(value); err != nil {
		return path.NewError(err)
	}
	return nil
}

func (p *PullPolicy) ToCtyValue() cty.Value {
	if p == nil {
		return cty.NullVal(cty.DynamicPseudoType)
	}
	switch *p {
	case "", "false":
		return cty.False
	case "true":
		return cty.True
	default:
		return cty.StringVal(string(*p))
	}
}
