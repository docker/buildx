package buildflags

import (
	"sync"

	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/convert"
	"github.com/zclconf/go-cty/cty/gocty"
)

var resourcesType = sync.OnceValue(func() cty.Type {
	return cty.ObjectWithOptionalAttrs(
		map[string]cty.Type{
			"memory":      cty.String,
			"memory-swap": cty.String,
			"cpu-shares":  cty.Number,
			"cpu-period":  cty.Number,
			"cpu-quota":   cty.Number,
			"cpuset-cpus": cty.String,
			"cpuset-mems": cty.String,
		},
		[]string{"memory", "memory-swap", "cpu-shares", "cpu-period", "cpu-quota", "cpuset-cpus", "cpuset-mems"},
	)
})

func (r *ResourcesConfig) FromCtyValue(in cty.Value, p cty.Path) error {
	conv, err := convert.Convert(in, resourcesType())
	if err != nil {
		return p.NewError(err)
	}

	if v := conv.GetAttr("memory"); !v.IsNull() && v.IsKnown() {
		s := v.AsString()
		r.Memory = &s
	}
	if v := conv.GetAttr("memory-swap"); !v.IsNull() && v.IsKnown() {
		s := v.AsString()
		r.MemorySwap = &s
	}
	if v := conv.GetAttr("cpu-shares"); !v.IsNull() && v.IsKnown() {
		var n int64
		if err := gocty.FromCtyValue(v, &n); err != nil {
			return p.NewError(err)
		}
		r.CPUShares = &n
	}
	if v := conv.GetAttr("cpu-period"); !v.IsNull() && v.IsKnown() {
		var n int64
		if err := gocty.FromCtyValue(v, &n); err != nil {
			return p.NewError(err)
		}
		r.CPUPeriod = &n
	}
	if v := conv.GetAttr("cpu-quota"); !v.IsNull() && v.IsKnown() {
		var n int64
		if err := gocty.FromCtyValue(v, &n); err != nil {
			return p.NewError(err)
		}
		r.CPUQuota = &n
	}
	if v := conv.GetAttr("cpuset-cpus"); !v.IsNull() && v.IsKnown() {
		s := v.AsString()
		r.CPUSetCPUs = &s
	}
	if v := conv.GetAttr("cpuset-mems"); !v.IsNull() && v.IsKnown() {
		s := v.AsString()
		r.CPUSetMems = &s
	}
	return nil
}

func (r *ResourcesConfig) ToCtyValue() cty.Value {
	if r == nil {
		return cty.NullVal(resourcesType())
	}

	vals := map[string]cty.Value{
		"memory":      cty.NullVal(cty.String),
		"memory-swap": cty.NullVal(cty.String),
		"cpu-shares":  cty.NullVal(cty.Number),
		"cpu-period":  cty.NullVal(cty.Number),
		"cpu-quota":   cty.NullVal(cty.Number),
		"cpuset-cpus": cty.NullVal(cty.String),
		"cpuset-mems": cty.NullVal(cty.String),
	}
	if r.Memory != nil {
		vals["memory"] = cty.StringVal(*r.Memory)
	}
	if r.MemorySwap != nil {
		vals["memory-swap"] = cty.StringVal(*r.MemorySwap)
	}
	if r.CPUShares != nil {
		vals["cpu-shares"] = cty.NumberIntVal(*r.CPUShares)
	}
	if r.CPUPeriod != nil {
		vals["cpu-period"] = cty.NumberIntVal(*r.CPUPeriod)
	}
	if r.CPUQuota != nil {
		vals["cpu-quota"] = cty.NumberIntVal(*r.CPUQuota)
	}
	if r.CPUSetCPUs != nil {
		vals["cpuset-cpus"] = cty.StringVal(*r.CPUSetCPUs)
	}
	if r.CPUSetMems != nil {
		vals["cpuset-mems"] = cty.StringVal(*r.CPUSetMems)
	}
	return cty.ObjectVal(vals)
}
