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

func (o *CacheOptions) fromCtyValue(in cty.Value, p cty.Path) (retErr error) {
	*o = make([]*CacheOptionsEntry, 0, in.LengthInt())

	yield := func(value cty.Value) bool {
		// Special handling for a string type to handle ref only format.
		if value.Type() == cty.String {
			var entries CacheOptions
			entries, retErr = ParseCacheEntry([]string{value.AsString()})
			if retErr != nil {
				return false
			}
			*o = append(*o, entries...)
			return true
		}

		entry := &CacheOptionsEntry{}
		if retErr = entry.FromCtyValue(value, p); retErr != nil {
			return false
		}
		*o = append(*o, entry)
		return true
	}
	eachElement(in)(yield)
	return retErr
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

func (e *CacheOptionsEntry) FromCtyValue(in cty.Value, p cty.Path) error {
	conv, err := convert.Convert(in, cty.Map(cty.String))
	if err != nil {
		return err
	}

	m := conv.AsValueMap()
	if err := getAndDelete(m, "type", &e.Type); err != nil {
		return err
	}
	e.Attrs = asMap(m)
	return e.validate(in)
}

func (e *CacheOptionsEntry) ToCtyValue() cty.Value {
	if e == nil {
		return cty.NullVal(cty.Map(cty.String))
	}

	vals := make(map[string]cty.Value, len(e.Attrs)+1)
	for k, v := range e.Attrs {
		vals[k] = cty.StringVal(v)
	}
	vals["type"] = cty.StringVal(e.Type)
	return cty.MapVal(vals)
}
