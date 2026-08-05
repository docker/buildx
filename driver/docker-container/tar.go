package docker

import (
	"archive/tar"
	"io"
	"io/fs"
	"os"

	"github.com/pkg/errors"
)

const (
	tarDirMode  int64 = 0o755
	tarFileMode int64 = 0o644
)

func tarDirectory(srcPath string) (io.ReadCloser, error) {
	stat, err := os.Lstat(srcPath)
	if err != nil {
		return nil, err
	}
	if !stat.IsDir() {
		return nil, errors.Errorf("%s is not a directory", srcPath)
	}
	root, err := os.OpenRoot(srcPath)
	if err != nil {
		return nil, err
	}

	pr, pw := io.Pipe()
	tw := tar.NewWriter(pw)
	go func() {
		defer root.Close()
		err := fs.WalkDir(root.FS(), ".", func(name string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if name == "." && d.IsDir() {
				return nil
			}
			return writeTarFile(root, tw, name, d)
		})
		if err == nil {
			err = tw.Close()
		}
		if err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		_ = pw.Close()
	}()
	return pr, nil
}

func writeTarFile(root *os.Root, tw *tar.Writer, name string, d fs.DirEntry) error {
	fi, err := d.Info()
	if err != nil {
		return err
	}

	mode := fi.Mode()
	hdr := &tar.Header{
		Name:    name,
		Mode:    tarFileMode,
		ModTime: fi.ModTime(),
	}
	switch {
	case fi.IsDir():
		hdr.Typeflag = tar.TypeDir
		hdr.Name += "/"
		hdr.Mode = tarDirMode
	case mode.IsRegular():
		hdr.Typeflag = tar.TypeReg
		hdr.Size = fi.Size()
	default:
		return errors.Errorf("unsupported file type %s", name)
	}

	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	if !mode.IsRegular() || hdr.Size == 0 {
		return nil
	}

	f, err := root.Open(name)
	if err != nil {
		return err
	}
	if _, err := io.Copy(tw, f); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
