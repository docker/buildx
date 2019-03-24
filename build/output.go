package build

import (
	"encoding/csv"
	"os"
	"strings"

	"github.com/moby/buildkit/client"
	"github.com/pkg/errors"
)

func ParseOutputs(inp []string) ([]client.ExportEntry, error) {
	var outs []client.ExportEntry
	if len(inp) == 0 {
		return nil, nil
	}
	for _, s := range inp {
		csvReader := csv.NewReader(strings.NewReader(s))
		fields, err := csvReader.Read()
		if err != nil {
			return nil, err
		}
		if len(fields) == 1 && fields[0] == s {
			outs = append(outs, client.ExportEntry{
				Type:      "local",
				OutputDir: s,
			})
			continue
		}

		out := client.ExportEntry{
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
				out.Type = value
			default:
				out.Attrs[key] = value
			}
		}
		if out.Type == "" {
			return nil, errors.Errorf("type is required for output")
		}

		// handle client side
		switch out.Type {
		case "local":
			dest, ok := out.Attrs["dest"]
			if !ok {
				return nil, errors.Errorf("dest is required for local output")
			}
			out.OutputDir = dest
			delete(out.Attrs, "dest")
		case "oci", "dest":
			dest, ok := out.Attrs["dest"]
			if !ok {
				if out.Type != "docker" {
					return nil, errors.Errorf("dest is required for %s output", out.Type)
				}
			} else {
				if dest == "-" {
					out.Output = os.Stdout
				} else {
					f, err := os.Open(dest)
					if err != nil {
						out.Output = f
					}
				}
				delete(out.Attrs, "dest")
			}
		case "registry":
			out.Type = "iamge"
			out.Attrs["push"] = "true"
		}

		outs = append(outs, out)
	}
	return outs, nil
}
