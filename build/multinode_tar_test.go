package build

import (
	"archive/tar"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/moby/buildkit/client"
	"github.com/stretchr/testify/require"
)

type testTarEntry struct {
	name string
	typ  byte
	data string
}

func TestPrepareMultiNodeTarExport(t *testing.T) {
	t.Run("no tar exporter", func(t *testing.T) {
		reqs := []*reqForNode{
			{so: &client.SolveOpt{Exports: []client.ExportEntry{{Type: client.ExporterOCI, Attrs: map[string]string{}}}}},
			{so: &client.SolveOpt{Exports: []client.ExportEntry{{Type: client.ExporterOCI, Attrs: map[string]string{}}}}},
		}
		exp, err := prepareMultiNodeTarExport(reqs)
		require.NoError(t, err)
		require.Nil(t, exp)
	})

	t.Run("rewrites exports to node files", func(t *testing.T) {
		exp, reqs := newTestMultiNodeTarExport(t, filepath.Join(t.TempDir(), "out.tar"), 2)
		require.Len(t, exp.nodeFiles, 2)
		require.NotEqual(t, exp.nodeFiles[0], exp.nodeFiles[1])
		for i, req := range reqs {
			w, err := req.so.Exports[0].Output(nil)
			require.NoError(t, err)
			require.NoError(t, w.Close())
			require.FileExists(t, exp.nodeFiles[i])
		}

		require.NoError(t, exp.Close())
		_, err := os.Stat(exp.tmpDir)
		require.True(t, os.IsNotExist(err))
	})

	t.Run("multiple tar exporters", func(t *testing.T) {
		out := func(map[string]string) (io.WriteCloser, error) { return nil, nil }
		reqs := make([]*reqForNode, 2)
		for i := range reqs {
			reqs[i] = &reqForNode{so: &client.SolveOpt{Exports: []client.ExportEntry{
				{Type: client.ExporterTar, Attrs: map[string]string{}, Output: out},
				{Type: client.ExporterTar, Attrs: map[string]string{}, Output: out},
			}}}
		}
		_, err := prepareMultiNodeTarExport(reqs)
		require.ErrorContains(t, err, "multiple tar exporters")
	})
}

func TestMultiNodeTarExportMerge(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.tar")
	exp, reqs := newTestMultiNodeTarExport(t, out, 2)

	writeTestNodeTar(t, reqs[0], []testTarEntry{
		{name: "linux_amd64/", typ: tar.TypeDir},
		{name: "linux_amd64/foo", typ: tar.TypeReg, data: "amd64"},
		{name: "linux_amd64/provenance.json", typ: tar.TypeReg, data: "{}"},
	})
	writeTestNodeTar(t, reqs[1], []testTarEntry{
		{name: "linux_arm64/", typ: tar.TypeDir},
		{name: "linux_arm64/foo", typ: tar.TypeReg, data: "arm64"},
		{name: "linux_arm64/provenance.json", typ: tar.TypeReg, data: "{}"},
	})

	require.NoError(t, exp.Merge(context.Background()))

	got := readTestTar(t, out)
	require.Equal(t, []testTarEntry{
		{name: "linux_amd64/", typ: tar.TypeDir},
		{name: "linux_amd64/foo", typ: tar.TypeReg, data: "amd64"},
		{name: "linux_amd64/provenance.json", typ: tar.TypeReg, data: "{}"},
		{name: "linux_arm64/", typ: tar.TypeDir},
		{name: "linux_arm64/foo", typ: tar.TypeReg, data: "arm64"},
		{name: "linux_arm64/provenance.json", typ: tar.TypeReg, data: "{}"},
	}, got)
}

func TestMultiNodeTarExportMergeSharedDir(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.tar")
	exp, reqs := newTestMultiNodeTarExport(t, out, 2)

	// a node building several platforms, or a frontend that adds a common
	// parent directory, may produce the same directory in several tarballs
	writeTestNodeTar(t, reqs[0], []testTarEntry{
		{name: "./", typ: tar.TypeDir},
		{name: "./linux_amd64/", typ: tar.TypeDir},
		{name: "./linux_amd64/foo", typ: tar.TypeReg, data: "amd64"},
	})
	writeTestNodeTar(t, reqs[1], []testTarEntry{
		{name: "./", typ: tar.TypeDir},
		{name: "./linux_arm64/", typ: tar.TypeDir},
		{name: "./linux_arm64/foo", typ: tar.TypeReg, data: "arm64"},
	})

	require.NoError(t, exp.Merge(context.Background()))

	got := readTestTar(t, out)
	require.Equal(t, []testTarEntry{
		{name: "./", typ: tar.TypeDir},
		{name: "./linux_amd64/", typ: tar.TypeDir},
		{name: "./linux_amd64/foo", typ: tar.TypeReg, data: "amd64"},
		{name: "./linux_arm64/", typ: tar.TypeDir},
		{name: "./linux_arm64/foo", typ: tar.TypeReg, data: "arm64"},
	}, got)
}

func TestMultiNodeTarExportMergeConflict(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.tar")
	exp, reqs := newTestMultiNodeTarExport(t, out, 2)

	// results that are not split by platform would overwrite each other
	writeTestNodeTar(t, reqs[0], []testTarEntry{{name: "foo", typ: tar.TypeReg, data: "amd64"}})
	writeTestNodeTar(t, reqs[1], []testTarEntry{{name: "./foo", typ: tar.TypeReg, data: "arm64"}})

	require.ErrorContains(t, exp.Merge(context.Background()), `conflicting path "foo" exported by multiple nodes`)
}

func newTestMultiNodeTarExport(t *testing.T, out string, nodes int) (*multiNodeTarExport, []*reqForNode) {
	t.Helper()

	output := func(map[string]string) (io.WriteCloser, error) {
		return os.Create(out)
	}
	reqs := make([]*reqForNode, nodes)
	for i := range reqs {
		reqs[i] = &reqForNode{so: &client.SolveOpt{Exports: []client.ExportEntry{{
			Type:   client.ExporterTar,
			Attrs:  map[string]string{},
			Output: output,
		}}}}
	}
	exp, err := prepareMultiNodeTarExport(reqs)
	require.NoError(t, err)
	require.NotNil(t, exp)
	t.Cleanup(func() { _ = exp.Close() })
	return exp, reqs
}

// writeTestNodeTar writes a tarball through the rewritten output of a node,
// like the tar exporter of the node would.
func writeTestNodeTar(t *testing.T, req *reqForNode, entries []testTarEntry) {
	t.Helper()

	w, err := req.so.Exports[0].Output(nil)
	require.NoError(t, err)
	tw := tar.NewWriter(w)
	for _, e := range entries {
		hdr := &tar.Header{Name: e.name, Typeflag: e.typ, Mode: 0o644, Size: int64(len(e.data))}
		if e.typ == tar.TypeDir {
			hdr.Mode = 0o755
		}
		require.NoError(t, tw.WriteHeader(hdr))
		_, err := tw.Write([]byte(e.data))
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, w.Close())
}

func readTestTar(t *testing.T, file string) []testTarEntry {
	t.Helper()

	f, err := os.Open(file)
	require.NoError(t, err)
	defer f.Close()

	var entries []testTarEntry
	tr := tar.NewReader(f)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		dt, err := io.ReadAll(tr)
		require.NoError(t, err)
		entries = append(entries, testTarEntry{name: hdr.Name, typ: hdr.Typeflag, data: string(dt)})
	}
	return entries
}
