package buildflags

import (
	"sync"

	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/convert"
)

var cacheOptionsEntryType = sync.OnceValue(func() cty.Type {
	return cty.Map(cty.String)
})

func (o *CacheOptions) FromCtyValue(in cty.Value, p cty.Path) error {
	got := in.Type()
	if got.IsTupleType() || got.IsListType() {
		return o.fromCtyValue(in, p)
	}

	want := cty.List(cacheOptionsEntryType())
	return p.NewErrorf("%s", convert.MismatchMessage(got, want))
}

func (o *CacheOptions) fromCtyValue(in cty.Value, p cty.Path) error {
	*o = make([]*CacheOptionsEntry, 0, in.LengthInt())
	for elem := in.ElementIterator(); elem.Next(); {
		_, value := elem.Element()

		entry := &CacheOptionsEntry{}
		if err := entry.FromCtyValue(value, p); err != nil {
			return err
		}
		*o = append(*o, entry)
	}
	return nil
}

func (o CacheOptions) ToCtyValue() cty.Value {
	if len(o) == 0 {
		return cty.ListValEmpty(cacheOptionsEntryType())
	}

	vals := make([]cty.Value, len(o))
	for i, entry := range o {
		vals[i] = entry.ToCtyValue()
	}
	return cty.ListVal(vals)
}

func (o *CacheOptionsEntry) FromCtyValue(in cty.Value, p cty.Path) error {
	if in.Type() == cty.String {
		if err := o.UnmarshalText([]byte(in.AsString())); err != nil {
			return p.NewError(err)
		}
		return nil
	}

	conv, err := convert.Convert(in, cty.Map(cty.String))
	if err != nil {
		return err
	}

	m := conv.AsValueMap()
	if err := getAndDelete(m, "type", &o.Type); err != nil {
		return err
	}
	o.Attrs = asMap(m)
	return o.validate(in)
}

func (o *CacheOptionsEntry) ToCtyValue() cty.Value {
	if o == nil {
		return cty.NullVal(cty.Map(cty.String))
	}

	vals := make(map[string]cty.Value, len(o.Attrs)+1)
	for k, v := range o.Attrs {
		vals[k] = cty.StringVal(v)
	}
	vals["type"] = cty.StringVal(o.Type)
	return cty.MapVal(vals)
}
