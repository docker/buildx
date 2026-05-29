package buildflags

import (
	"strconv"

	"github.com/pkg/errors"
)

// ResourcesConfig holds the cgroup resource limits applied to individual build
// steps (RUN instructions). It maps to the `--resource` build flags.
type ResourcesConfig struct {
	Memory     *string `json:"memory,omitempty"`
	MemorySwap *string `json:"memory-swap,omitempty"`
	CPUShares  *int64  `json:"cpu-shares,omitempty"`
	CPUPeriod  *int64  `json:"cpu-period,omitempty"`
	CPUQuota   *int64  `json:"cpu-quota,omitempty"`
	CPUSetCPUs *string `json:"cpuset-cpus,omitempty"`
	CPUSetMems *string `json:"cpuset-mems,omitempty"`
}

func (r *ResourcesConfig) Merge(other *ResourcesConfig) *ResourcesConfig {
	if r == nil {
		r = &ResourcesConfig{}
	}
	merged := *r
	if other != nil {
		if other.Memory != nil {
			merged.Memory = other.Memory
		}
		if other.MemorySwap != nil {
			merged.MemorySwap = other.MemorySwap
		}
		if other.CPUShares != nil {
			merged.CPUShares = other.CPUShares
		}
		if other.CPUPeriod != nil {
			merged.CPUPeriod = other.CPUPeriod
		}
		if other.CPUQuota != nil {
			merged.CPUQuota = other.CPUQuota
		}
		if other.CPUSetCPUs != nil {
			merged.CPUSetCPUs = other.CPUSetCPUs
		}
		if other.CPUSetMems != nil {
			merged.CPUSetMems = other.CPUSetMems
		}
	}
	return &merged
}

// ToEntries renders the config as `key=value` entries for build.ParseResourceLimits.
func (r *ResourcesConfig) ToEntries() []string {
	if r == nil {
		return nil
	}
	var entries []string
	if r.Memory != nil {
		entries = append(entries, "memory="+*r.Memory)
	}
	if r.MemorySwap != nil {
		entries = append(entries, "memory-swap="+*r.MemorySwap)
	}
	if r.CPUShares != nil {
		entries = append(entries, "cpu-shares="+strconv.FormatInt(*r.CPUShares, 10))
	}
	if r.CPUPeriod != nil {
		entries = append(entries, "cpu-period="+strconv.FormatInt(*r.CPUPeriod, 10))
	}
	if r.CPUQuota != nil {
		entries = append(entries, "cpu-quota="+strconv.FormatInt(*r.CPUQuota, 10))
	}
	if r.CPUSetCPUs != nil {
		entries = append(entries, "cpuset-cpus="+*r.CPUSetCPUs)
	}
	if r.CPUSetMems != nil {
		entries = append(entries, "cpuset-mems="+*r.CPUSetMems)
	}
	return entries
}

// SetField sets a single resource field by name, used by bake `--set` overrides.
func (r *ResourcesConfig) SetField(name, value string) error {
	switch name {
	case "memory":
		r.Memory = &value
	case "memory-swap":
		r.MemorySwap = &value
	case "cpu-shares", "cpu-period", "cpu-quota":
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return errors.Errorf("invalid value %s for int64 key resources.%s", value, name)
		}
		switch name {
		case "cpu-shares":
			r.CPUShares = &n
		case "cpu-period":
			r.CPUPeriod = &n
		case "cpu-quota":
			r.CPUQuota = &n
		}
	case "cpuset-cpus":
		r.CPUSetCPUs = &value
	case "cpuset-mems":
		r.CPUSetMems = &value
	default:
		return errors.Errorf("unknown resources key %s", name)
	}
	return nil
}
