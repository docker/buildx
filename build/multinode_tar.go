package build

import (
	"archive/tar"
	"context"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/session/filesync"
	"github.com/pkg/errors"
)

// multiNodeTarExport tracks a tar exporter of a build that is split across
// several nodes.
//
// Multi-node builds always request a multi-platform result, so the tarball
// of every node already places the files of each platform in its own
// subdirectory (for example "linux_amd64/" and "linux_arm64/"). Each node
// writes its tarball to a temporary file and, once all nodes are done, the
// entries of all tarballs are written to the original destination as a
// single tarball.
type multiNodeTarExport struct {
	output filesync.FileOutputFunc

	tmpDir    string
	nodeFiles []string
}

// prepareMultiNodeTarExport rewrites the tar exporter of every node so it
// writes to a node specific temporary file. It returns nil if the build has
// no tar exporter.
func prepareMultiNodeTarExport(reqs []*reqForNode) (_ *multiNodeTarExport, err error) {
	var exp *multiNodeTarExport
	defer func() {
		if err != nil && exp != nil {
			_ = exp.Close()
		}
	}()

	for i, req := range reqs {
		idx := -1
		for j, e := range req.so.Exports {
			if e.Type != client.ExporterTar {
				continue
			}
			if idx != -1 {
				return nil, errors.Errorf("multiple tar exporters for multi-node builds currently not supported")
			}
			idx = j
		}
		if idx == -1 {
			if exp != nil {
				return nil, errors.Errorf("tar exporter is not configured for all nodes")
			}
			continue
		}

		e := &req.so.Exports[idx]
		if exp == nil {
			if i != 0 {
				return nil, errors.Errorf("tar exporter is not configured for all nodes")
			}
			if e.Output == nil {
				return nil, errors.Errorf("tar exporter for multi-node builds requires an output file")
			}
			tmpDir, err := os.MkdirTemp("", "buildx-multinode-tar-")
			if err != nil {
				return nil, errors.Wrap(err, "failed to create temporary directory for multi-node tar export")
			}
			exp = &multiNodeTarExport{
				output: e.Output,
				tmpDir: tmpDir,
			}
		}

		file := filepath.Join(exp.tmpDir, strconv.Itoa(i)+".tar")
		exp.nodeFiles = append(exp.nodeFiles, file)
		e.Output = func(map[string]string) (io.WriteCloser, error) {
			return os.Create(file)
		}
	}
	return exp, nil
}

// Close removes the temporary tarballs written by the nodes.
func (e *multiNodeTarExport) Close() error {
	if e == nil || e.tmpDir == "" {
		return nil
	}
	return os.RemoveAll(e.tmpDir)
}

// Merge writes the entries of the tarballs of all nodes to the original
// destination of the tar exporter. Directories that exist in several
// tarballs are written once; any other path that exists in several
// tarballs is an error.
func (e *multiNodeTarExport) Merge(ctx context.Context) (err error) {
	w, err := e.output(nil)
	if err != nil {
		return err
	}
	defer func() {
		if err1 := w.Close(); err == nil {
			err = err1
		}
	}()

	tw := tar.NewWriter(w)
	seen := map[string]byte{}
	for _, f := range e.nodeFiles {
		if err := appendTar(ctx, tw, f, seen); err != nil {
			return err
		}
	}
	return tw.Close()
}

func appendTar(ctx context.Context, tw *tar.Writer, file string, seen map[string]byte) error {
	f, err := os.Open(file)
	if err != nil {
		return errors.Wrap(err, "failed to open node tarball")
	}
	defer f.Close()

	tr := tar.NewReader(f)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return errors.Wrap(err, "failed to read node tarball")
		}

		name := path.Clean("/" + strings.TrimPrefix(hdr.Name, "./"))
		if typ, ok := seen[name]; ok {
			if typ == tar.TypeDir && hdr.Typeflag == tar.TypeDir {
				continue
			}
			return errors.Errorf("conflicting path %q exported by multiple nodes", strings.TrimPrefix(name, "/"))
		}
		seen[name] = hdr.Typeflag

		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if _, err := io.Copy(tw, tr); err != nil {
			return err
		}
	}
}
