package build

import (
	"encoding/csv"
	"strings"

	"github.com/moby/buildkit/client"
	"github.com/pkg/errors"
)

func ParseCacheEntry(in []string) ([]client.CacheOptionsEntry, error) {
	imports := make([]client.CacheOptionsEntry, 0, len(in))
	for _, in := range in {
		csvReader := csv.NewReader(strings.NewReader(in))
		fields, err := csvReader.Read()
		if err != nil {
			return nil, err
		}
		if isRefOnlyFormat(fields) {
			for _, field := range fields {
				imports = append(imports, client.CacheOptionsEntry{
					Type:  "registry",
					Attrs: map[string]string{"ref": field},
				})
			}
			continue
		}
		im := client.CacheOptionsEntry{
			Attrs: map[string]string{},
		}
		for _, field := range fields {
			parts := strings.SplitN(field, "=", 2)
			if len(parts) != 2 {
				return nil, errors.Errorf("invalid value %s", field)
			}
			key := strings.ToLower(parts[0])
			value := parts[1]
			switch key {
			case "type":
				im.Type = value
			default:
				im.Attrs[key] = value
			}
		}
		if im.Type == "" {
			return nil, errors.Errorf("type required form> %q", in)
		}
		imports = append(imports, im)
	}
	return imports, nil
}

func isRefOnlyFormat(in []string) bool {
	for _, v := range in {
		if strings.Contains(v, "=") {
			return false
		}
	}
	return true
}
