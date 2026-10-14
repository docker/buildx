package bundle

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/containerd/containerd/v2/core/content"
	imgarchive "github.com/containerd/containerd/v2/core/images/archive"
	"github.com/moby/buildkit/util/contentutil"
	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
)

func writeTestBlob(t *testing.T, buf contentutil.Buffer, mt string, dt []byte) ocispecs.Descriptor {
	t.Helper()

	desc := ocispecs.Descriptor{
		MediaType: mt,
		Digest:    digest.FromBytes(dt),
		Size:      int64(len(dt)),
	}
	require.NoError(t, content.WriteBlob(context.TODO(), buf, desc.Digest.String(), bytes.NewReader(dt), desc))
	return desc
}

func TestArchiveExportReadsLayerOnce(t *testing.T) {
	buf := contentutil.NewBuffer()

	logs := writeTestBlob(t, buf, "application/vnd.buildkit.status.v0", bytes.Repeat([]byte("#5 [stage 3/9] RUN apt-get install\n"), 52000))
	config := writeTestBlob(t, buf, HistoryRecordMediaTypeV0, []byte(`{}`))
	mfst, err := json.Marshal(ocispecs.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ocispecs.MediaTypeImageManifest,
		Config:    config,
		Layers:    []ocispecs.Descriptor{logs},
	})
	require.NoError(t, err)
	mfstDesc := writeTestBlob(t, buf, ocispecs.MediaTypeImageManifest, mfst)

	p := newCountingProvider(buf)
	err = imgarchive.Export(context.TODO(), &chunkedProvider{p}, io.Discard, imgarchive.WithManifest(mfstDesc), imgarchive.WithSkipDockerManifest())
	require.NoError(t, err)
	require.Equal(t, 1, p.reads[logs.Digest])
}
