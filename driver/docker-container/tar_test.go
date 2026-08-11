package docker

import (
	"archive/tar"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

type tarEntry struct {
	header *tar.Header
	body   []byte
}

func TestTarConfigFiles(t *testing.T) {
	files := map[string][]byte{
		"buildkitd.toml":           []byte("debug = true\n"),
		"certs/example.com/ca.pem": []byte("certificate"),
	}

	rc, err := tarConfigFiles(files)
	require.NoError(t, err)
	defer rc.Close()

	expected := []string{
		"buildkit/",
		"buildkit/certs/",
		"buildkit/certs/example.com/",
		"buildkit/buildkitd.toml",
		"buildkit/certs/example.com/ca.pem",
	}
	entries, names := readTarEntries(t, rc)
	require.Equal(t, expected, names)
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

func TestTarConfigFilesSnapshotsContent(t *testing.T) {
	files := map[string][]byte{
		"buildkitd.toml": []byte("debug = true\n"),
	}

	rc, err := tarConfigFiles(files)
	require.NoError(t, err)
	defer rc.Close()

	files["buildkitd.toml"][0] = '#'

	entries, _ := readTarEntries(t, rc)
	require.Equal(t, []byte("debug = true\n"), entries["buildkit/buildkitd.toml"].body)
}

func TestTarConfigFilesEmpty(t *testing.T) {
	rc, err := tarConfigFiles(nil)
	require.NoError(t, err)
	defer rc.Close()

	tr := tar.NewReader(rc)
	_, err = tr.Next()
	require.ErrorIs(t, err, io.EOF)
}

func TestTarConfigFilesRejectsInvalidPaths(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
	}{
		{name: "empty", path: ""},
		{name: "dot", path: "."},
		{name: "leading-dot", path: "./buildkitd.toml"},
		{name: "parent", path: "../buildkitd.toml"},
		{name: "absolute", path: "/buildkitd.toml"},
		{name: "trailing-slash", path: "certs/"},
		{name: "parent-element", path: "certs/../ca.pem"},
		{name: "dot-element", path: "certs/./ca.pem"},
		{name: "empty-element", path: "certs//ca.pem"},
		{name: "backslash", path: `certs\ca.pem`},
		{name: "nul", path: "certs/ca\x00.pem"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rc, err := tarConfigFiles(map[string][]byte{
				tc.path: []byte("invalid"),
			})
			require.Error(t, err)
			require.Contains(t, err.Error(), "invalid config file path")
			require.Nil(t, rc)
		})
	}
}

func TestTarConfigFilesRejectsFileDirectoryConflict(t *testing.T) {
	rc, err := tarConfigFiles(map[string][]byte{
		"certs":        []byte("file"),
		"certs/ca.pem": []byte("ca"),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "conflicts with directory")
	require.Nil(t, rc)
}

func TestConfigTarEntriesRejectsInvalidRoot(t *testing.T) {
	for _, tc := range []struct {
		name string
		root string
	}{
		{name: "empty", root: ""},
		{name: "dot", root: "."},
		{name: "parent", root: "../buildkit"},
		{name: "absolute", root: "/buildkit"},
		{name: "nested", root: "etc/buildkit"},
		{name: "backslash", root: `etc\buildkit`},
		{name: "nul", root: "build\x00kit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entries, dirs, err := configTarEntries(tc.root, map[string][]byte{
				"buildkitd.toml": []byte("debug = true\n"),
			})
			require.Error(t, err)
			require.Contains(t, err.Error(), "invalid config archive root")
			require.Nil(t, entries)
			require.Nil(t, dirs)
		})
	}
}

func readTarEntries(t *testing.T, r io.Reader) (map[string]tarEntry, []string) {
	t.Helper()

	tr := tar.NewReader(r)
	entries := map[string]tarEntry{}
	var names []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		names = append(names, hdr.Name)
		body, err := io.ReadAll(tr)
		require.NoError(t, err)
		entries[hdr.Name] = tarEntry{
			header: new(*hdr),
			body:   body,
		}
	}
	return entries, names
}
