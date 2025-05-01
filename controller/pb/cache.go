package pb

import (
	"maps"

	"github.com/moby/buildkit/client"
)

type CacheOptionsEntry struct {
	Type  string
	Attrs map[string]string
}

func CreateCaches(entries []*CacheOptionsEntry) []client.CacheOptionsEntry {
	var outs []client.CacheOptionsEntry
	if len(entries) == 0 {
		return nil
	}
	for _, entry := range entries {
		out := client.CacheOptionsEntry{
			Type:  entry.Type,
			Attrs: map[string]string{},
		}
		maps.Copy(out.Attrs, entry.Attrs)
		outs = append(outs, out)
	}
	return outs
}
