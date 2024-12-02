package pb

import (
	"io"
	"os"
	"strconv"

	"github.com/containerd/console"
	"github.com/moby/buildkit/client"
	"github.com/pkg/errors"
)

func CreateExports(entries []*ExportEntry) ([]client.ExportEntry, []string, error) {
	var outs []client.ExportEntry
	var localPaths []string
	if len(entries) == 0 {
		return nil, nil, nil
	}
	var stdoutUsed bool
	for _, entry := range entries {
		if entry.Type == "" {
			return nil, nil, errors.Errorf("type is required for output")
		}

		out := client.ExportEntry{
			Type:  entry.Type,
			Attrs: map[string]string{},
		}
		for k, v := range entry.Attrs {
			out.Attrs[k] = v
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
			out.Attrs["push"] = "true"
		}

		if supportDir {
			if entry.Destination == "" {
				return nil, nil, errors.Errorf("dest is required for %s exporter", out.Type)
			}
			if entry.Destination == "-" {
				return nil, nil, errors.Errorf("dest cannot be stdout for %s exporter", out.Type)
			}

			fi, err := os.Stat(entry.Destination)
			if err != nil && !os.IsNotExist(err) {
				return nil, nil, errors.Wrapf(err, "invalid destination directory: %s", entry.Destination)
			}
			if err == nil && !fi.IsDir() {
				return nil, nil, errors.Errorf("destination directory %s is a file", entry.Destination)
			}
			out.OutputDir = entry.Destination
			localPaths = append(localPaths, entry.Destination)
		}
		if supportFile {
			if entry.Destination == "" && out.Type != client.ExporterDocker {
				entry.Destination = "-"
			}
			if entry.Destination == "-" {
				if stdoutUsed {
					return nil, nil, errors.Errorf("multiple outputs configured to write to stdout")
				}
				if _, err := console.ConsoleFromFile(os.Stdout); err == nil {
					return nil, nil, errors.Errorf("dest file is required for %s exporter. refusing to write to console", out.Type)
				}
				out.Output = wrapWriteCloser(os.Stdout)
				stdoutUsed = true
			} else if entry.Destination != "" {
				fi, err := os.Stat(entry.Destination)
				if err != nil && !os.IsNotExist(err) {
					return nil, nil, errors.Wrapf(err, "invalid destination file: %s", entry.Destination)
				}
				if err == nil && fi.IsDir() {
					return nil, nil, errors.Errorf("destination file %s is a directory", entry.Destination)
				}
				f, err := os.Create(entry.Destination)
				if err != nil {
					return nil, nil, errors.Errorf("failed to open %s", err)
				}
				out.Output = wrapWriteCloser(f)
				localPaths = append(localPaths, entry.Destination)
			}
		}

		outs = append(outs, out)
	}
	return outs, localPaths, nil
}

func wrapWriteCloser(wc io.WriteCloser) func(map[string]string) (io.WriteCloser, error) {
	return func(map[string]string) (io.WriteCloser, error) {
		return wc, nil
	}
}
