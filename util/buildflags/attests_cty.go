package buildflags

import (
	"strconv"
	"sync"

	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/convert"
)

var attestType = sync.OnceValue(func() cty.Type {
	return cty.Map(cty.String)
})

func (a *Attests) FromCtyValue(in cty.Value, p cty.Path) error {
	got := in.Type()
	if got.IsTupleType() || got.IsListType() {
		return a.fromCtyValue(in, p)
	}

	want := cty.List(attestType())
	return p.NewErrorf("%s", convert.MismatchMessage(got, want))
}

func (a *Attests) fromCtyValue(in cty.Value, p cty.Path) (retErr error) {
	*a = make([]*Attest, 0, in.LengthInt())

	yield := func(value cty.Value) bool {
		entry := &Attest{}
		if retErr = entry.FromCtyValue(value, p); retErr != nil {
			return false
		}
		*a = append(*a, entry)
		return true
	}
	eachElement(in)(yield)
	return retErr
}

func (a Attests) ToCtyValue() cty.Value {
	if len(a) == 0 {
		return cty.ListValEmpty(attestType())
	}

	vals := make([]cty.Value, len(a))
	for i, entry := range a {
		vals[i] = entry.ToCtyValue()
	}
	return cty.ListVal(vals)
}

func (a *Attest) FromCtyValue(in cty.Value, p cty.Path) error {
	if in.Type() == cty.String {
		if err := a.UnmarshalText([]byte(in.AsString())); err != nil {
			return p.NewError(err)
		}
		return nil
	}

	conv, err := convert.Convert(in, cty.Map(cty.String))
	if err != nil {
		return err
	}

	a.Attrs = map[string]string{}
	for it := conv.ElementIterator(); it.Next(); {
		k, v := it.Element()
		if !v.IsKnown() {
			continue
		}

		switch key := k.AsString(); key {
		case "type":
			a.Type = v.AsString()
		case "disabled":
			b, err := strconv.ParseBool(v.AsString())
			if err != nil {
				return err
			}
			a.Disabled = b
		default:
			a.Attrs[key] = v.AsString()
		}
	}
	return nil
}

func (a *Attest) ToCtyValue() cty.Value {
	if a == nil {
		return cty.NullVal(cty.Map(cty.String))
	}

	vals := make(map[string]cty.Value, len(a.Attrs)+2)
	for k, v := range a.Attrs {
		vals[k] = cty.StringVal(v)
	}
	vals["type"] = cty.StringVal(a.Type)
	if a.Disabled {
		vals["disabled"] = cty.StringVal("true")
	}
	return cty.MapVal(vals)
}
