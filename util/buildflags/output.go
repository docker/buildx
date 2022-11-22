package buildflags

import (
	"encoding/csv"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/containerd/console"
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

		out := client.ExportEntry{
			Attrs: map[string]string{},
		}
		if len(fields) == 1 && fields[0] == s && !strings.HasPrefix(s, "type=") {
			if s != "-" {
				outs = append(outs, client.ExportEntry{
					Type:      client.ExporterLocal,
					OutputDir: s,
				})
				continue
			}
			out = client.ExportEntry{
				Type: client.ExporterTar,
				Attrs: map[string]string{
					"dest": s,
				},
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

		supportFile := false
		supportDir := false
		switch out.Type {
		case client.ExporterLocal:
			supportDir = true
		case client.ExporterTar:
			supportFile = true
		case client.ExporterOCI, client.ExporterDocker:
			tar, err := strconv.ParseBool(out.Attrs["tar"])
			if err != nil {
				tar = true
			}
			supportFile = tar
			supportDir = !tar
		case "registry":
			out.Type = client.ExporterImage
			if _, ok := out.Attrs["push"]; !ok {
				out.Attrs["push"] = "true"
			}
		}

		dest, ok := out.Attrs["dest"]
		if supportDir {
			if !ok {
				return nil, errors.Errorf("dest is required for %s exporter", out.Type)
			}
			if dest == "-" {
				return nil, errors.Errorf("dest cannot be stdout for %s exporter", out.Type)
			}

			fi, err := os.Stat(dest)
			if err != nil && !os.IsNotExist(err) {
				return nil, errors.Wrapf(err, "invalid destination directory: %s", dest)
			}
			if err == nil && !fi.IsDir() {
				return nil, errors.Errorf("destination directory %s is a file", dest)
			}
			out.OutputDir = dest
		}
		if supportFile {
			if !ok && out.Type != client.ExporterDocker {
				dest = "-"
			}
			if dest == "-" {
				if _, err := console.ConsoleFromFile(os.Stdout); err == nil {
					return nil, errors.Errorf("dest file is required for %s exporter. refusing to write to console", out.Type)
				}
				out.Output = wrapWriteCloser(os.Stdout)
			} else if dest != "" {
				fi, err := os.Stat(dest)
				if err != nil && !os.IsNotExist(err) {
					return nil, errors.Wrapf(err, "invalid destination file: %s", dest)
				}
				if err == nil && fi.IsDir() {
					return nil, errors.Errorf("destination file %s is a directory", dest)
				}
				f, err := os.Create(dest)
				if err != nil {
					return nil, errors.Errorf("failed to open %s", err)
				}
				out.Output = wrapWriteCloser(f)
			}
		}
		if supportFile || supportDir {
			delete(out.Attrs, "dest")
		}

		outs = append(outs, out)
	}
	return outs, nil
}

func wrapWriteCloser(wc io.WriteCloser) func(map[string]string) (io.WriteCloser, error) {
	return func(map[string]string) (io.WriteCloser, error) {
		return wc, nil
	}
}
