package buildflags

import (
	"encoding"
	"sync"

	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/convert"
	"github.com/zclconf/go-cty/cty/gocty"
)

func (e *ExportEntry) FromCtyValue(in cty.Value, p cty.Path) error {
	conv, err := convert.Convert(in, cty.Map(cty.String))
	if err == nil {
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
	return unmarshalTextFallback(in, e, err)
}

func (e *ExportEntry) ToCtyValue() cty.Value {
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

var secretType = sync.OnceValue(func() cty.Type {
	return cty.ObjectWithOptionalAttrs(
		map[string]cty.Type{
			"id":  cty.String,
			"src": cty.String,
			"env": cty.String,
		},
		[]string{"id", "src", "env"},
	)
})

func (e *Secret) FromCtyValue(in cty.Value, p cty.Path) (err error) {
	conv, err := convert.Convert(in, secretType())
	if err == nil {
		if id := conv.GetAttr("id"); !id.IsNull() {
			e.ID = id.AsString()
		}
		if src := conv.GetAttr("src"); !src.IsNull() {
			e.FilePath = src.AsString()
		}
		if env := conv.GetAttr("env"); !env.IsNull() {
			e.Env = env.AsString()
		}
		return nil
	}
	return unmarshalTextFallback(in, e, err)
}

func (e *Secret) ToCtyValue() cty.Value {
	if e == nil {
		return cty.NullVal(secretType())
	}

	return cty.ObjectVal(map[string]cty.Value{
		"id":  cty.StringVal(e.ID),
		"src": cty.StringVal(e.FilePath),
		"env": cty.StringVal(e.Env),
	})
}

var sshType = sync.OnceValue(func() cty.Type {
	return cty.ObjectWithOptionalAttrs(
		map[string]cty.Type{
			"id":    cty.String,
			"paths": cty.List(cty.String),
		},
		[]string{"id", "paths"},
	)
})

func (e *SSH) FromCtyValue(in cty.Value, p cty.Path) (err error) {
	conv, err := convert.Convert(in, sshType())
	if err == nil {
		if id := conv.GetAttr("id"); !id.IsNull() {
			e.ID = id.AsString()
		}
		if paths := conv.GetAttr("paths"); !paths.IsNull() {
			if err := gocty.FromCtyValue(paths, &e.Paths); err != nil {
				return err
			}
		}
		return nil
	}
	return unmarshalTextFallback(in, e, err)
}

func (e *SSH) ToCtyValue() cty.Value {
	if e == nil {
		return cty.NullVal(sshType())
	}

	var ctyPaths cty.Value
	if len(e.Paths) > 0 {
		paths := make([]cty.Value, len(e.Paths))
		for i, path := range e.Paths {
			paths[i] = cty.StringVal(path)
		}
		ctyPaths = cty.ListVal(paths)
	} else {
		ctyPaths = cty.ListValEmpty(cty.String)
	}

	return cty.ObjectVal(map[string]cty.Value{
		"id":    cty.StringVal(e.ID),
		"paths": ctyPaths,
	})
}

func getAndDelete(m map[string]cty.Value, attr string, gv interface{}) error {
	if v, ok := m[attr]; ok {
		delete(m, attr)
		return gocty.FromCtyValue(v, gv)
	}
	return nil
}

func asMap(m map[string]cty.Value) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v.AsString()
	}
	return out
}

func unmarshalTextFallback[V encoding.TextUnmarshaler](in cty.Value, v V, inErr error) (outErr error) {
	// Attempt to convert this type to a string.
	conv, err := convert.Convert(in, cty.String)
	if err != nil {
		// Cannot convert. Do not attempt to convert and return the original error.
		return inErr
	}

	// Conversion was successful. Use UnmarshalText on the string and return any
	// errors associated with that.
	return v.UnmarshalText([]byte(conv.AsString()))
}
