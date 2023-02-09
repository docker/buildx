package pb

import "github.com/moby/buildkit/client"

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
		for k, v := range entry.Attrs {
			out.Attrs[k] = v
		}
		outs = append(outs, out)
	}
	return outs
}
