package buildflags

import (
	"encoding/csv"
	"regexp"
	"strings"

	"github.com/containerd/containerd/platforms"
	controllerapi "github.com/docker/buildx/controller/pb"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/exporter/containerimage/exptypes"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
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

func ParseAnnotations(inp []string) (map[exptypes.AnnotationKey]string, error) {
	// TODO: use buildkit's annotation parser once it supports setting custom prefix and ":" separator
	annotationRegexp := regexp.MustCompile(`^(?:([a-z-]+)(?:\[([A-Za-z0-9_/-]+)\])?:)?(\S+)$`)
	annotations := make(map[exptypes.AnnotationKey]string)
	for _, inp := range inp {
		k, v, ok := strings.Cut(inp, "=")
		if !ok {
			return nil, errors.Errorf("invalid annotation %q, expected key=value", inp)
		}

		groups := annotationRegexp.FindStringSubmatch(k)
		if groups == nil {
			return nil, errors.Errorf("invalid annotation format, expected <type>:<key>=<value>, got %q", inp)
		}

		typ, platform, key := groups[1], groups[2], groups[3]
		switch typ {
		case "":
		case exptypes.AnnotationIndex, exptypes.AnnotationIndexDescriptor, exptypes.AnnotationManifest, exptypes.AnnotationManifestDescriptor:
		default:
			return nil, errors.Errorf("unknown annotation type %q", typ)
		}

		var ociPlatform *ocispecs.Platform
		if platform != "" {
			p, err := platforms.Parse(platform)
			if err != nil {
				return nil, errors.Wrapf(err, "invalid platform %q", platform)
			}
			ociPlatform = &p
		}

		ak := exptypes.AnnotationKey{
			Type:     typ,
			Platform: ociPlatform,
			Key:      key,
		}
		annotations[ak] = v
	}
	return annotations, nil
}
