package buildflags

import (
	"encoding/csv"
	"strings"

	controllerapi "github.com/docker/buildx/controller/pb"
	"github.com/moby/buildkit/client"
	"github.com/pkg/errors"
)

func ParseExports(inp []string) ([]*controllerapi.ExportEntry, error) {
	var outs []*controllerapi.ExportEntry
	if len(inp) == 0 {
		return nil, nil
	}
	for _, s := range inp {
		csvReader := csv.NewReader(strings.NewReader(s))
		fields, err := csvReader.Read()
		if err != nil {
			return nil, err
		}

		out := controllerapi.ExportEntry{
			Attrs: map[string]string{},
		}
		if len(fields) == 1 && fields[0] == s && !strings.HasPrefix(s, "type=") {
			if s != "-" {
				outs = append(outs, &controllerapi.ExportEntry{
					Type:        client.ExporterLocal,
					Destination: s,
				})
				continue
			}
			out = controllerapi.ExportEntry{
				Type:        client.ExporterTar,
				Destination: s,
			}
		}

		if out.Type == "" {
			for _, field := range fields {
				parts := strings.SplitN(field, "=", 2)
				if len(parts) != 2 {
					return nil, errors.Errorf("invalid value %s", field)
				}
				key := strings.TrimSpace(strings.ToLower(parts[0]))
				value := parts[1]
				switch key {
				case "type":
					out.Type = value
				default:
					out.Attrs[key] = value
				}
			}
		}
		if out.Type == "" {
			return nil, errors.Errorf("type is required for output")
		}

		if out.Type == "registry" {
			out.Type = client.ExporterImage
			if _, ok := out.Attrs["push"]; !ok {
				out.Attrs["push"] = "true"
			}
		}

		if dest, ok := out.Attrs["dest"]; ok {
			out.Destination = dest
			delete(out.Attrs, "dest")
		}

		outs = append(outs, &out)
	}
	return outs, nil
}
