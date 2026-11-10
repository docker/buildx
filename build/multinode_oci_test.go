package build

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/containerd/containerd/v2/core/content"
	contentlocal "github.com/containerd/containerd/v2/plugins/content/local"
	"github.com/containerd/platforms"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/client/ociindex"
	"github.com/moby/buildkit/exporter/containerimage/exptypes"
	digest "github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
)

func TestPrepareMultiNodeOCIExport(t *testing.T) {
	t.Run("no oci exporter", func(t *testing.T) {
		reqs := []*reqForNode{
			{so: &client.SolveOpt{Exports: []client.ExportEntry{{Type: client.ExporterImage, Attrs: map[string]string{}}}}},
			{so: &client.SolveOpt{Exports: []client.ExportEntry{{Type: client.ExporterImage, Attrs: map[string]string{}}}}},
		}
		exp, err := prepareMultiNodeOCIExport(reqs)
		require.NoError(t, err)
		require.Nil(t, exp)
	})

	t.Run("rewrites exports to node layouts", func(t *testing.T) {
		out := func(map[string]string) (io.WriteCloser, error) { return nil, nil }
		reqs := make([]*reqForNode, 2)
		for i := range reqs {
			reqs[i] = &reqForNode{so: &client.SolveOpt{Exports: []client.ExportEntry{{
				Type:   client.ExporterOCI,
				Attrs:  map[string]string{"name": "docker.io/foo/bar:v1, docker.io/foo/bar:latest"},
				Output: out,
			}}}}
		}
		exp, err := prepareMultiNodeOCIExport(reqs)
		require.NoError(t, err)
		require.NotNil(t, exp)
		t.Cleanup(func() { _ = exp.Close() })

		require.NotNil(t, exp.output)
		require.Empty(t, exp.outputDir)
		require.Equal(t, []string{"docker.io/foo/bar:v1", "docker.io/foo/bar:latest"}, exp.names)
		require.Len(t, exp.nodeDirs, 2)
		require.NotEqual(t, exp.nodeDirs[0], exp.nodeDirs[1])
		for i, req := range reqs {
			e := req.so.Exports[0]
			require.Nil(t, e.Output)
			require.Equal(t, exp.nodeDirs[i], e.OutputDir)
			require.Equal(t, "false", e.Attrs["tar"])
		}

		require.NoError(t, exp.Close())
		_, err = os.Stat(exp.tmpDir)
		require.True(t, os.IsNotExist(err))
	})

	t.Run("multiple oci exporters", func(t *testing.T) {
		reqs := make([]*reqForNode, 2)
		for i := range reqs {
			reqs[i] = &reqForNode{so: &client.SolveOpt{Exports: []client.ExportEntry{
				{Type: client.ExporterOCI, Attrs: map[string]string{}, OutputDir: t.TempDir()},
				{Type: client.ExporterOCI, Attrs: map[string]string{}, OutputDir: t.TempDir()},
			}}}
		}
		_, err := prepareMultiNodeOCIExport(reqs)
		require.ErrorContains(t, err, "multiple oci exporters")
	})
}

func TestMultiNodeOCIExportMergeDir(t *testing.T) {
	ctx := context.Background()
	dest := t.TempDir()

	exp, res := newTestMultiNodeOCIExport(t, "", dest, []string{"docker.io/foo/bar:v1"})

	desc, err := exp.Merge(ctx, res, nil)
	require.NoError(t, err)
	require.Equal(t, ocispecs.MediaTypeImageIndex, desc.MediaType)

	idx, err := ociindex.NewStoreIndex(dest).Read()
	require.NoError(t, err)
	require.Len(t, idx.Manifests, 1)
	require.Equal(t, desc.Digest, idx.Manifests[0].Digest)
	require.Equal(t, "docker.io/foo/bar:v1", idx.Manifests[0].Annotations["io.containerd.image.name"])
	require.Equal(t, "v1", idx.Manifests[0].Annotations[ocispecs.AnnotationRefName])

	store, err := contentlocal.NewStore(dest)
	require.NoError(t, err)
	requireMergedIndex(ctx, t, store, desc)
}

func TestMultiNodeOCIExportMergeArchive(t *testing.T) {
	ctx := context.Background()
	archive := filepath.Join(t.TempDir(), "out.tar")

	exp, res := newTestMultiNodeOCIExport(t, archive, "", []string{"docker.io/foo/bar:v1"})

	desc, err := exp.Merge(ctx, res, nil)
	require.NoError(t, err)

	f, err := os.Open(archive)
	require.NoError(t, err)
	defer f.Close()

	files := map[string][]byte{}
	tr := tar.NewReader(f)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		dt, err := io.ReadAll(tr)
		require.NoError(t, err)
		files[hdr.Name] = dt
	}

	require.Contains(t, files, ocispecs.ImageLayoutFile)
	require.NotContains(t, files, "manifest.json")

	var idx ocispecs.Index
	require.NoError(t, json.Unmarshal(files[ocispecs.ImageIndexFile], &idx))
	require.Len(t, idx.Manifests, 1)
	require.Equal(t, desc.Digest, idx.Manifests[0].Digest)
	require.Equal(t, "docker.io/foo/bar:v1", idx.Manifests[0].Annotations["io.containerd.image.name"])

	// every blob referenced by the merged index must be part of the archive
	store, err := contentlocal.NewStore(filepath.Join(exp.tmpDir, "merged"))
	require.NoError(t, err)
	for _, dgst := range requireMergedIndex(ctx, t, store, desc) {
		require.Contains(t, files, "blobs/sha256/"+dgst.Encoded())
	}
}

func TestMultiNodeOCIExportMergeIndexAnnotations(t *testing.T) {
	ctx := context.Background()
	dest := t.TempDir()

	exp, res := newTestMultiNodeOCIExport(t, "", dest, nil)

	ann := map[exptypes.AnnotationKey]string{
		{Type: exptypes.AnnotationIndex, Key: "org.opencontainers.image.title"}: "test",
	}
	desc, err := exp.Merge(ctx, res, ann)
	require.NoError(t, err)

	store, err := contentlocal.NewStore(dest)
	require.NoError(t, err)
	dt, err := content.ReadBlob(ctx, store, desc)
	require.NoError(t, err)
	var idx ocispecs.Index
	require.NoError(t, json.Unmarshal(dt, &idx))
	require.Equal(t, "test", idx.Annotations["org.opencontainers.image.title"])

	// without names the index is tagged as latest, like a single node build
	layoutIdx, err := ociindex.NewStoreIndex(dest).Read()
	require.NoError(t, err)
	require.Len(t, layoutIdx.Manifests, 1)
	require.Equal(t, "latest", layoutIdx.Manifests[0].Annotations[ocispecs.AnnotationRefName])
}

// newTestMultiNodeOCIExport prepares a multi-node oci export for two nodes
// and populates the node layouts like the oci exporter of each node would.
func newTestMultiNodeOCIExport(t *testing.T, archive, outputDir string, names []string) (*multiNodeOCIExport, []*client.SolveResponse) {
	t.Helper()

	var output func(map[string]string) (io.WriteCloser, error)
	if archive != "" {
		output = func(map[string]string) (io.WriteCloser, error) {
			return os.Create(archive)
		}
	}
	nodePlatforms := []string{"linux/amd64", "linux/arm64"}
	reqs := make([]*reqForNode, len(nodePlatforms))
	for i := range reqs {
		reqs[i] = &reqForNode{so: &client.SolveOpt{Exports: []client.ExportEntry{{
			Type:      client.ExporterOCI,
			Attrs:     map[string]string{"name": strings.Join(names, ",")},
			Output:    output,
			OutputDir: outputDir,
		}}}}
	}
	exp, err := prepareMultiNodeOCIExport(reqs)
	require.NoError(t, err)
	t.Cleanup(func() { _ = exp.Close() })

	res := make([]*client.SolveResponse, len(nodePlatforms))
	for i, p := range nodePlatforms {
		desc := writeTestNodeLayout(t, reqs[i].so.Exports[0].OutputDir, platforms.MustParse(p), names)
		dt, err := json.Marshal(desc)
		require.NoError(t, err)
		res[i] = &client.SolveResponse{ExporterResponse: map[string]string{
			exptypes.ExporterImageDigestKey:     desc.Digest.String(),
			exptypes.ExporterImageDescriptorKey: base64.StdEncoding.EncodeToString(dt),
		}}
	}
	return exp, res
}

// writeTestNodeLayout writes a single platform image wrapped in an image
// index (multi-platform is always forced for multi-node builds) to an OCI
// layout and returns the descriptor of the index.
func writeTestNodeLayout(t *testing.T, dir string, p ocispecs.Platform, names []string) ocispecs.Descriptor {
	t.Helper()
	ctx := context.Background()

	store, err := contentlocal.NewStore(dir)
	require.NoError(t, err)

	write := func(mt string, v any) ocispecs.Descriptor {
		var dt []byte
		if b, ok := v.([]byte); ok {
			dt = b
		} else {
			dt, err = json.Marshal(v)
			require.NoError(t, err)
		}
		desc := ocispecs.Descriptor{MediaType: mt, Digest: digest.FromBytes(dt), Size: int64(len(dt))}
		require.NoError(t, content.WriteBlob(ctx, store, desc.Digest.String(), bytes.NewReader(dt), desc))
		return desc
	}

	layer := write(ocispecs.MediaTypeImageLayer, []byte("layer-"+platforms.Format(p)))
	config := write(ocispecs.MediaTypeImageConfig, ocispecs.Image{
		Platform: p,
		RootFS:   ocispecs.RootFS{Type: "layers", DiffIDs: []digest.Digest{layer.Digest}},
	})
	mfst := write(ocispecs.MediaTypeImageManifest, ocispecs.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ocispecs.MediaTypeImageManifest,
		Config:    config,
		Layers:    []ocispecs.Descriptor{layer},
	})
	mfst.Platform = &p
	idx := write(ocispecs.MediaTypeImageIndex, ocispecs.Index{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ocispecs.MediaTypeImageIndex,
		Manifests: []ocispecs.Descriptor{mfst},
	})

	tags := []ociindex.NameOrTag{ociindex.Tag("latest")}
	if len(names) > 0 {
		tags = tags[:0]
		for _, n := range names {
			tags = append(tags, ociindex.Name(n))
		}
	}
	require.NoError(t, ociindex.NewStoreIndex(dir).Put(idx, tags...))
	return idx
}

// requireMergedIndex checks that the merged index contains the manifests of
// both nodes and that all of their blobs exist in the store. It returns the
// digests of all blobs reachable from the index.
func requireMergedIndex(ctx context.Context, t *testing.T, store content.Store, desc ocispecs.Descriptor) []digest.Digest {
	t.Helper()

	dt, err := content.ReadBlob(ctx, store, desc)
	require.NoError(t, err)
	var idx ocispecs.Index
	require.NoError(t, json.Unmarshal(dt, &idx))
	require.Len(t, idx.Manifests, 2)

	var got []string
	dgsts := []digest.Digest{desc.Digest}
	for _, m := range idx.Manifests {
		require.NotNil(t, m.Platform)
		got = append(got, platforms.Format(*m.Platform))

		dt, err := content.ReadBlob(ctx, store, m)
		require.NoError(t, err)
		var mfst ocispecs.Manifest
		require.NoError(t, json.Unmarshal(dt, &mfst))
		dgsts = append(dgsts, m.Digest, mfst.Config.Digest)
		for _, d := range append([]ocispecs.Descriptor{mfst.Config}, mfst.Layers...) {
			_, err := store.Info(ctx, d.Digest)
			require.NoError(t, err, "missing blob %s", d.Digest)
			if d.Digest != mfst.Config.Digest {
				dgsts = append(dgsts, d.Digest)
			}
		}
	}
	require.ElementsMatch(t, []string{"linux/amd64", "linux/arm64"}, got)
	return dgsts
}
