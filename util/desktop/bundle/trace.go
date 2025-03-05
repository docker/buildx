package bundle

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"regexp"
	"strings"

	"github.com/containerd/containerd/v2/core/content"
	"github.com/docker/buildx/util/otelutil"
	"github.com/moby/buildkit/util/contentutil"
	"github.com/opencontainers/go-digest"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"go.opentelemetry.io/otel/attribute"
)

var (
	sensitiveKeys = []string{"ghtoken", "token", "access_key_id", "secret_access_key", "session_token"}
	reAttrs       = regexp.MustCompile(`(?i)(` + strings.Join(sensitiveKeys, "|") + `)=[^ ,]+`)
	reGhs         = regexp.MustCompile(`ghs_[A-Za-z0-9]{36}`)
)

func sanitizeCommand(value string) string {
	value = reAttrs.ReplaceAllString(value, "${1}=xxxxx")
	// reGhs is just double proofing. Not really needed.
	value = reGhs.ReplaceAllString(value, "xxxxx")
	return value
}

func sanitizeTrace(ctx context.Context, mp *contentutil.MultiProvider, desc ocispecs.Descriptor) (*ocispecs.Descriptor, error) {
	ra, err := mp.ReaderAt(ctx, desc)
	if err != nil {
		return nil, err
	}
	defer ra.Close()

	buf := &bytes.Buffer{}
	dec := json.NewDecoder(io.NewSectionReader(ra, 0, ra.Size()))
	enc := json.NewEncoder(buf)
	enc.SetIndent("", "  ")
	for {
		var obj otelutil.Span
		if err := dec.Decode(&obj); err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}

		for i, att := range obj.Attributes {
			v := att.Value
			if v.Type() == attribute.STRING {
				obj.Attributes[i].Value = attribute.StringValue(sanitizeCommand(v.AsString()))
			}
		}

		if err := enc.Encode(obj); err != nil {
			return nil, err
		}
	}

	buffer := contentutil.NewBuffer()
	newDesc := ocispecs.Descriptor{
		MediaType: desc.MediaType,
		Size:      int64(buf.Len()),
		Digest:    digest.FromBytes(buf.Bytes()),
	}
	if err := content.WriteBlob(ctx, buffer, "trace-sanitized", bytes.NewReader(buf.Bytes()), newDesc); err != nil {
		return nil, err
	}

	mp.Add(newDesc.Digest, buffer)

	return &newDesc, nil
}
