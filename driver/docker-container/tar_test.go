package docker

import (
	"archive/tar"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

type tarEntry struct {
	header *tar.Header
	body   []byte
}

func TestTarDirectoryConfigFiles(t *testing.T) {
	files := map[string][]byte{
		"buildkitd.toml":           []byte("debug = true\n"),
		"certs/example.com/ca.pem": []byte("certificate"),
	}
	srcPath, err := writeConfigFiles(files)
	require.NoError(t, err)
	defer os.RemoveAll(srcPath)

	rc, err := tarDirectory(srcPath)
	require.NoError(t, err)
	defer rc.Close()

	entries := readTarEntries(t, rc)
	expected := []string{
		"buildkit/",
		"buildkit/buildkitd.toml",
		"buildkit/certs/",
		"buildkit/certs/example.com/",
		"buildkit/certs/example.com/ca.pem",
	}
	require.Len(t, entries, len(expected))
	for _, name := range expected {
		entry, ok := entries[name]
		require.Truef(t, ok, "missing archive entry %q", name)
		require.Equal(t, 0, entry.header.Uid)
		require.Equal(t, 0, entry.header.Gid)
		require.Empty(t, entry.header.Uname)
		require.Empty(t, entry.header.Gname)
	}

	require.Equal(t, byte(tar.TypeDir), entries["buildkit/"].header.Typeflag)
	require.Equal(t, tarDirMode, entries["buildkit/"].header.Mode)
	require.Equal(t, files["buildkitd.toml"], entries["buildkit/buildkitd.toml"].body)
	require.Equal(t, tarFileMode, entries["buildkit/buildkitd.toml"].header.Mode)
	require.Equal(t, files["certs/example.com/ca.pem"], entries["buildkit/certs/example.com/ca.pem"].body)
}

func TestTarDirectoryEmpty(t *testing.T) {
	rc, err := tarDirectory(t.TempDir())
	require.NoError(t, err)
	defer rc.Close()

	tr := tar.NewReader(rc)
	_, err = tr.Next()
	require.ErrorIs(t, err, io.EOF)
}

func readTarEntries(t *testing.T, r io.Reader) map[string]tarEntry {
	t.Helper()

	tr := tar.NewReader(r)
	entries := map[string]tarEntry{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		body, err := io.ReadAll(tr)
		require.NoError(t, err)
		hdrCopy := *hdr
		entries[hdr.Name] = tarEntry{
			header: &hdrCopy,
			body:   body,
		}
	}
	return entries
}
