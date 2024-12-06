package buildflags

import (
	"sync"

	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/convert"
)

var exportEntryType = sync.OnceValue(func() cty.Type {
	return cty.Map(cty.String)
})

func (e *Exports) FromNativeValue(in cty.Value, p cty.Path) error {
	got := in.Type()
	if got.IsTupleType() || got.IsListType() {
		return e.fromNativeValue(in, p)
	}

	want := cty.List(exportEntryType())
	return p.NewErrorf("%s", convert.MismatchMessage(got, want))
}

func (e *Exports) fromNativeValue(in cty.Value, p cty.Path) error {
	*e = make([]*ExportEntry, 0, in.LengthInt())
	for elem := in.ElementIterator(); elem.Next(); {
		_, value := elem.Element()

		entry := &ExportEntry{}
		if err := entry.FromNativeValue(value, p); err != nil {
			return err
		}
		*e = append(*e, entry)
	}
	return nil
}

func (e Exports) ToNativeValue() cty.Value {
	if len(e) == 0 {
		return cty.ListValEmpty(exportEntryType())
	}

	vals := make([]cty.Value, len(e))
	for i, entry := range e {
		vals[i] = entry.ToNativeValue()
	}
	return cty.ListVal(vals)
}

func (e *ExportEntry) FromNativeValue(in cty.Value, p cty.Path) error {
	if in.Type() == cty.String {
		if err := e.UnmarshalText([]byte(in.AsString())); err != nil {
			return p.NewError(err)
		}
		return nil
	}

	conv, err := convert.Convert(in, cty.Map(cty.String))
	if err != nil {
		return err
	}

	m := conv.AsValueMap()
	if err := getAndDelete(m, "type", &e.Type); err != nil {
		return err
	}
	if err := getAndDelete(m, "dest", &e.Destination); err != nil {
		return err
	}
	e.Attrs = asMap(m)
	return e.validate()
}

func (e *ExportEntry) ToNativeValue() cty.Value {
	if e == nil {
		return cty.NullVal(cty.Map(cty.String))
	}

	vals := make(map[string]cty.Value, len(e.Attrs)+2)
	for k, v := range e.Attrs {
		vals[k] = cty.StringVal(v)
	}
	vals["type"] = cty.StringVal(e.Type)
	vals["dest"] = cty.StringVal(e.Destination)
	return cty.MapVal(vals)
}
