package docker

import (
	"archive/tar"
	"bytes"
	"io"
	"io/fs"
	"maps"
	"path"
	"slices"
	"strings"

	"github.com/docker/buildx/util/confutil"
	"github.com/pkg/errors"
)

const (
	tarDirMode  int64 = 0o755
	tarFileMode int64 = 0o644
)

type configTarEntry struct {
	name string
	data []byte
}

func tarConfigFiles(files map[string][]byte) (io.ReadCloser, error) {
	entries, dirs, err := configTarEntries(path.Base(confutil.DefaultBuildKitConfigDir), files)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := writeConfigTar(tw, dirs, entries); err != nil {
		_ = tw.Close()
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(buf.Bytes())), nil
}

func configTarEntries(configDir string, files map[string][]byte) ([]configTarEntry, []string, error) {
	configDir, err := configArchiveRoot(configDir)
	if err != nil {
		return nil, nil, err
	}

	entries := make([]configTarEntry, 0, len(files))
	dirs := map[string]struct{}{}
	filePaths := make(map[string]struct{}, len(files))
	if len(files) > 0 {
		dirs[configDir] = struct{}{}
	}

	for _, name := range slices.Sorted(maps.Keys(files)) {
		archivePath, err := configArchivePath(configDir, name)
		if err != nil {
			return nil, nil, err
		}
		entries = append(entries, configTarEntry{
			name: archivePath,
			data: files[name],
		})
		filePaths[archivePath] = struct{}{}
		for dir := path.Dir(archivePath); dir != "."; dir = path.Dir(dir) {
			dirs[dir] = struct{}{}
		}
	}

	for filePath := range filePaths {
		if _, ok := dirs[filePath]; ok {
			return nil, nil, errors.Errorf("config file path %q conflicts with directory", strings.TrimPrefix(filePath, configDir+"/"))
		}
	}

	return entries, slices.Sorted(maps.Keys(dirs)), nil
}

func configArchiveRoot(name string) (string, error) {
	if name == "." || strings.Contains(name, "/") || strings.Contains(name, `\`) || strings.ContainsRune(name, 0) || !fs.ValidPath(name) {
		return "", errors.Errorf("invalid config archive root %q", name)
	}
	return name, nil
}

func configArchivePath(configDir, name string) (string, error) {
	if strings.Contains(name, `\`) || strings.ContainsRune(name, 0) {
		return "", errors.Errorf("invalid config file path %q", name)
	}
	// Validate the literal slash path. path.Join would clean traversal first.
	archivePath := configDir + "/" + name
	if !fs.ValidPath(archivePath) {
		return "", errors.Errorf("invalid config file path %q", name)
	}
	return archivePath, nil
}

func writeConfigTar(tw *tar.Writer, dirs []string, entries []configTarEntry) error {
	for _, dir := range dirs {
		if err := tw.WriteHeader(&tar.Header{
			Name:     dir + "/",
			Typeflag: tar.TypeDir,
			Mode:     tarDirMode,
		}); err != nil {
			return err
		}
	}
	for _, entry := range entries {
		if err := tw.WriteHeader(&tar.Header{
			Name:     entry.name,
			Typeflag: tar.TypeReg,
			Mode:     tarFileMode,
			Size:     int64(len(entry.data)),
		}); err != nil {
			return err
		}
		if len(entry.data) == 0 {
			continue
		}
		if _, err := tw.Write(entry.data); err != nil {
			return err
		}
	}
	return nil
}
