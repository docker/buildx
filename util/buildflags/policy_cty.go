package buildflags

import (
	"fmt"
	"math/big"
	"strconv"
	"sync"

	"github.com/pkg/errors"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/convert"
)

type PolicyConfigs []PolicyConfig

var policyConfigType = sync.OnceValue(func() cty.Type {
	return cty.Map(cty.String)
})

func (p *PolicyConfigs) FromCtyValue(in cty.Value, path cty.Path) error {
	got := in.Type()
	if got.IsTupleType() || got.IsListType() {
		return p.fromCtyValue(in, path)
	}

	want := cty.List(policyConfigType())
	return path.NewErrorf("%s", convert.MismatchMessage(got, want))
}

func (p *PolicyConfigs) fromCtyValue(in cty.Value, path cty.Path) (retErr error) {
	*p = make([]PolicyConfig, 0, in.LengthInt())

	yield := func(value cty.Value) bool {
		if value.Type() == cty.String {
			var cfg PolicyConfig
			cfg, retErr = ParsePolicyConfig(value.AsString())
			if retErr != nil {
				return false
			}
			*p = append(*p, cfg)
			return true
		}

		if value.Type().IsObjectType() || value.Type().IsMapType() {
			var cfg PolicyConfig
			cfg, retErr = policyConfigFromMap(value)
			if retErr != nil {
				return false
			}
			*p = append(*p, cfg)
			return true
		}

		retErr = path.NewErrorf("%s", convert.MismatchMessage(value.Type(), policyConfigType()))
		return false
	}
	eachElement(in)(yield)
	return retErr
}

func (p PolicyConfigs) ToCtyValue() cty.Value {
	if len(p) == 0 {
		return cty.ListValEmpty(policyConfigType())
	}

	vals := make([]cty.Value, len(p))
	for i, entry := range p {
		vals[i] = entry.ToCtyValue()
	}
	return cty.ListVal(vals)
}

func (p *PolicyConfig) FromCtyValue(in cty.Value, path cty.Path) error {
	if in.Type() == cty.String {
		cfg, err := ParsePolicyConfig(in.AsString())
		if err != nil {
			return path.NewError(err)
		}
		*p = cfg
		return nil
	}

	if in.Type().IsObjectType() || in.Type().IsMapType() {
		cfg, err := policyConfigFromMap(in)
		if err != nil {
			return path.NewError(err)
		}
		*p = cfg
		return nil
	}

	return path.NewErrorf("%s", convert.MismatchMessage(in.Type(), policyConfigType()))
}

func (p PolicyConfig) ToCtyValue() cty.Value {
	vals := map[string]cty.Value{}
	if len(p.Files) > 0 {
		vals["filename"] = cty.StringVal(p.Files[0].Filename)
	}
	if p.Reset {
		vals["reset"] = cty.StringVal(strconv.FormatBool(p.Reset))
	}
	if p.Disabled {
		vals["disabled"] = cty.StringVal(strconv.FormatBool(p.Disabled))
	}
	if p.Strict != nil {
		vals["strict"] = cty.StringVal(strconv.FormatBool(*p.Strict))
	}
	if p.LogLevel != nil {
		vals["log-level"] = cty.StringVal(p.LogLevel.String())
	}
	if len(vals) == 0 {
		return cty.MapValEmpty(cty.String)
	}
	return cty.MapVal(vals)
}

func policyConfigFromMap(in cty.Value) (PolicyConfig, error) {
	fields := make([]string, 0)
	for k, v := range in.AsValueMap() {
		if v.IsNull() || !v.IsKnown() {
			continue
		}
		if v.Type() == cty.String && v.AsString() == "" {
			continue
		}
		field, err := policyField(k, v)
		if err != nil {
			return PolicyConfig{}, err
		}
		fields = append(fields, field)
	}
	return parsePolicyFields(fields)
}

func policyField(key string, value cty.Value) (string, error) {
	switch value.Type() {
	case cty.String:
		return fmt.Sprintf("%s=%s", key, value.AsString()), nil
	case cty.Bool:
		return fmt.Sprintf("%s=%t", key, value.True()), nil
	case cty.Number:
		var f big.Float
		f.Set(value.AsBigFloat())
		return fmt.Sprintf("%s=%s", key, f.Text('f', -1)), nil
	default:
		return "", errors.Errorf("%s", convert.MismatchMessage(value.Type(), cty.String))
	}
}
