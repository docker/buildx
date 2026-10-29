package bundle

import (
	"archive/tar"
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

func TestArchiveExportChunkedReads(t *testing.T) {
	for _, tt := range []struct {
		name  string
		size  int
		reads int
	}{
		{name: "empty", size: 0, reads: 0},
		{name: "below chunk size", size: readChunkSize - 1, reads: 1},
		{name: "at chunk size", size: readChunkSize, reads: 1},
		{name: "above chunk size", size: readChunkSize + 1, reads: 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dt := make([]byte, tt.size)
			for i := range dt {
				dt[i] = byte(i % 251)
			}
			buf := contentutil.NewBuffer()
			logs := writeTestBlob(t, buf, "application/vnd.buildkit.status.v0", dt)
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
			var out bytes.Buffer
			err = imgarchive.Export(context.TODO(), &chunkedProvider{p}, &out, imgarchive.WithManifest(mfstDesc), imgarchive.WithSkipDockerManifest())
			require.NoError(t, err)
			require.Equal(t, tt.reads, p.reads[logs.Digest])

			tr := tar.NewReader(&out)
			for {
				hdr, err := tr.Next()
				require.NoError(t, err)
				if hdr.Name != "blobs/"+logs.Digest.Algorithm().String()+"/"+logs.Digest.Encoded() {
					continue
				}
				got, err := io.ReadAll(tr)
				require.NoError(t, err)
				require.Equal(t, dt, got)
				break
			}
		})
	}
}
