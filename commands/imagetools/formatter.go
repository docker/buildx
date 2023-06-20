package commands

import (
	"encoding/json"
	"fmt"

	"github.com/docker/buildx/util/imagetools"
	"github.com/docker/cli/cli/command/formatter"
	"github.com/docker/distribution/reference"
	"github.com/moby/buildkit/util/appcontext"
	"github.com/opencontainers/go-digest"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
)

const (
	tableFormat    = "table {{ .Name }}\t{{ .MediaType }}\t{{ .Digest }}"
	prettyTemplate = `{{- if .Name }}
Name:      {{ .Name }}
{{- end }}
{{- if .MediaType }}
MediaType: {{ .MediaType }}
{{- end }}
{{- if .Digest }}
Digest:    {{ .Digest }}
{{- end }}
{{- if .Manifests }}
Manifests:
{{- range $manifest := .Manifests }}
  {{ if $manifest.Name }}
  Name:       {{ $manifest.Name }}
  {{- end }}
  {{- if $manifest.MediaType }}
  MediaType:  {{ $manifest.MediaType }}
  {{- end }}
  {{- if $manifest.Platform }}
  Platform:   {{ $manifest.Platform }}
  {{- end }}
  {{- if $manifest.OSVersion }}
  OSVersion:  {{ $manifest.OSVersion }}
  {{- end }}
  {{- if $manifest.OSFeatures }}
  OSFeatures: {{ $manifest.OSFeatures }}
  {{- end }}
  {{- if $manifest.URLs }}
  URLs:       {{ $manifest.URLs }}
  {{- end }}
  {{- if $manifest.Annotations }}
  {{ range $key, $value := $manifest.Annotations }}
    {{ $key }}: {{ $value }}
  {{- end }}
  {{- end }}
{{- end }}
{{- end }}`
)

func makeFormat(source string) formatter.Format {
	switch source {
	case formatter.PrettyFormatKey:
		return prettyTemplate
	case formatter.TableFormatKey:
		return tableFormat
	case formatter.RawFormatKey:
		return formatter.JSONFormat
	}
	return formatter.Format(source)
}

func inspectFormatWrite(ctx formatter.Context, name string, opt imagetools.Opt) error {
	resolver := imagetools.New(opt)
	return ctx.Write(&inspectContext{
		HeaderContext: formatter.HeaderContext{
			Header: formatter.SubHeaderContext{
				"Name":      "NAME",
				"MediaType": "MEDIA TYPE",
				"Digest":    "DIGEST",
			},
		},
	}, func(format func(formatter.SubContext) error) error {
		ref, err := imagetools.ParseRef(name)
		if err != nil {
			return fmt.Errorf("parse ref: %w", err)
		}

		dt, mfst, err := resolver.Get(appcontext.Context(), ref.String())
		if err != nil {
			return err
		}

		var index ocispecs.Index
		if err := json.Unmarshal(dt, &index); err != nil {
			return err
		}

		return format(&inspectContext{
			ref:        ref,
			index:      index,
			descriptor: mfst,
			resolver:   resolver,
		})
	})
}

type inspectContext struct {
	formatter.HeaderContext
	ref        reference.Named
	index      ocispecs.Index
	descriptor ocispecs.Descriptor
	resolver   *imagetools.Resolver
}

func (ctx *inspectContext) MarshalJSON() ([]byte, error) {
	return formatter.MarshalJSON(ctx)
}

func (ctx *inspectContext) Name() string {
	return ctx.ref.String()
}

func (ctx *inspectContext) Manifest() manifestList {
	return manifestList{
		SchemaVersion: ctx.index.Versioned.SchemaVersion,
		MediaType:     ctx.index.MediaType,
		Digest:        ctx.descriptor.Digest,
		Size:          ctx.descriptor.Size,
		Manifests:     ctx.index.Manifests,
		Annotations:   ctx.descriptor.Annotations,
	}
}

func (ctx *inspectContext) Image() (*ocispecs.Image, error) {
	res, err := imagetools.
		NewLoader(ctx.resolver.Resolver()).
		Load(appcontext.Context(), ctx.ref.String())
	if err != nil {
		return nil, fmt.Errorf("load: %w", err)
	}
	var img *ocispecs.Image
	for _, v := range res.Configs() {
		img = v
		break
	}
	return img, nil
}

type manifestList struct {
	SchemaVersion int
	MediaType     string
	Digest        digest.Digest
	Size          int64
	Manifests     []ocispecs.Descriptor
	Annotations   map[string]string
}

// type inspectManifestContext struct {
// 	ref        reference.Named
// 	descriptor ocispecs.Descriptor
// }

// func (ctx *inspectManifestContext) MarshalJSON() ([]byte, error) {
// 	return formatter.MarshalJSON(ctx)
// }

// func (ctx *inspectManifestContext) Name() (string, error) {
// 	cc, err := reference.WithDigest(ctx.ref, ctx.descriptor.Digest)
// 	if err != nil {
// 		return "", fmt.Errorf("with digest: %w", err)
// 	}
// 	return cc.String(), nil
// }

// func (ctx *inspectManifestContext) MediaType() string {
// 	return ctx.descriptor.MediaType
// }

// func (ctx *inspectManifestContext) Platform() *string {
// 	if ctx.descriptor.Platform != nil {
// 		s := platforms.Format(*ctx.descriptor.Platform)
// 		return &s
// 	}
// 	return nil
// }

// func (ctx *inspectManifestContext) OSVersion() string {
// 	if ctx.descriptor.Platform != nil {
// 		return ctx.descriptor.Platform.OSVersion
// 	}
// 	return ""
// }

// func (ctx *inspectManifestContext) OSFeatures() []string {
// 	if ctx.descriptor.Platform != nil {
// 		return ctx.descriptor.Platform.OSFeatures
// 	}
// 	return nil
// }

// func (ctx *inspectManifestContext) URLs() []string {
// 	return ctx.descriptor.URLs
// }
