package commands

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/moby/buildkit/exporter/containerimage/exptypes"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
)

func TestParseValidateFlags(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		got, err := parseValidateFlags(nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.repro {
			t.Fatalf("expected repro=false")
		}
	})

	t.Run("repro", func(t *testing.T) {
		got, err := parseValidateFlags([]string{"repro"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !got.repro {
			t.Fatalf("expected repro=true")
		}
	})

	t.Run("unknown", func(t *testing.T) {
		_, err := parseValidateFlags([]string{"wat"})
		if err == nil {
			t.Fatalf("expected error")
		}
	})
}

func TestGetReproDigest(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		_, err := getReproDigest(context.Background(), nil, map[string]string{})
		if err == nil {
			t.Fatalf("expected error")
		}
	})

	t.Run("present", func(t *testing.T) {
		want := "sha256:deadbeef"
		got, err := getReproDigest(context.Background(), nil, map[string]string{
			exptypes.ExporterImageDigestKey: want,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("index_descriptor_without_name_errors", func(t *testing.T) {
		desc := ocispecs.Descriptor{
			MediaType: ocispecs.MediaTypeImageIndex,
			Digest:    "sha256:feedface",
			Size:      123,
		}
		dt, err := json.Marshal(desc)
		if err != nil {
			t.Fatalf("marshal descriptor: %v", err)
		}
		_, err = getReproDigest(context.Background(), nil, map[string]string{
			exptypes.ExporterImageDescriptorKey: base64.StdEncoding.EncodeToString(dt),
			exptypes.ExporterImageDigestKey:     "sha256:deadbeef",
		})
		if err == nil {
			t.Fatalf("expected error")
		}
	})
}
