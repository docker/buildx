package build

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/containerd/containerd/v2/core/content"
	imgarchive "github.com/containerd/containerd/v2/core/images/archive"
	contentlocal "github.com/containerd/containerd/v2/plugins/content/local"
	"github.com/docker/buildx/util/imagetools"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/client/ociindex"
	"github.com/moby/buildkit/exporter/containerimage/exptypes"
	"github.com/moby/buildkit/session/filesync"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
)

// multiNodeOCIExport tracks an oci exporter (including a docker exporter that
// was converted to oci because the daemon supports importing OCI archives)
// of a build that is split across several nodes.
//
// A single node can't produce the final multi-platform image on its own, so
// each node exports its result into a temporary OCI layout. Once all nodes
// are done, the per-node results are combined into a single image index on
// the client and written to the destination that was originally requested:
// an OCI layout directory (tar=false), or an OCI archive written to a file,
// stdout or the docker image loader.
type multiNodeOCIExport struct {
	output    filesync.FileOutputFunc
	outputDir string
	names     []string

	tmpDir   string
	nodeDirs []string
}

// prepareMultiNodeOCIExport rewrites the oci exporter of every node so it
// writes to a node specific temporary OCI layout. It returns nil if the
// build has no oci exporter.
func prepareMultiNodeOCIExport(reqs []*reqForNode) (_ *multiNodeOCIExport, err error) {
	var exp *multiNodeOCIExport
	defer func() {
		if err != nil && exp != nil {
			_ = exp.Close()
		}
	}()

	for i, req := range reqs {
		idx := -1
		for j, e := range req.so.Exports {
			if e.Type != client.ExporterOCI {
				continue
			}
			if idx != -1 {
				return nil, errors.Errorf("multiple oci exporters for multi-node builds currently not supported")
			}
			idx = j
		}
		if idx == -1 {
			if exp != nil {
				return nil, errors.Errorf("oci exporter is not configured for all nodes")
			}
			continue
		}

		e := &req.so.Exports[idx]
		if exp == nil {
			if i != 0 {
				return nil, errors.Errorf("oci exporter is not configured for all nodes")
			}
			if e.Output == nil && e.OutputDir == "" {
				return nil, errors.Errorf("oci exporter for multi-node builds requires an output file or directory")
			}
			tmpDir, err := os.MkdirTemp("", "buildx-multinode-oci-")
			if err != nil {
				return nil, errors.Wrap(err, "failed to create temporary directory for multi-node oci export")
			}
			exp = &multiNodeOCIExport{
				output:    e.Output,
				outputDir: e.OutputDir,
				tmpDir:    tmpDir,
			}
			if v := e.Attrs["name"]; v != "" {
				for n := range strings.SplitSeq(v, ",") {
					if n = strings.TrimSpace(n); n != "" {
						exp.names = append(exp.names, n)
					}
				}
			}
		}

		dir := filepath.Join(exp.tmpDir, strconv.Itoa(i))
		exp.nodeDirs = append(exp.nodeDirs, dir)

		e.Output = nil
		e.OutputDir = dir
		e.Attrs = maps.Clone(e.Attrs)
		if e.Attrs == nil {
			e.Attrs = map[string]string{}
		}
		e.Attrs["tar"] = "false"
	}
	return exp, nil
}

// Close removes the temporary OCI layouts used by the nodes.
func (e *multiNodeOCIExport) Close() error {
	if e == nil || e.tmpDir == "" {
		return nil
	}
	return os.RemoveAll(e.tmpDir)
}

// Merge combines the results of all nodes into a single image index and
// writes it to the original destination of the oci exporter. It returns the
// descriptor of the merged index.
func (e *multiNodeOCIExport) Merge(ctx context.Context, res []*client.SolveResponse, ann map[exptypes.AnnotationKey]string) (ocispecs.Descriptor, error) {
	if len(res) != len(e.nodeDirs) {
		return ocispecs.Descriptor{}, errors.Errorf("unexpected number of results for multi-node oci export: got %d, want %d", len(res), len(e.nodeDirs))
	}

	r := imagetools.New(imagetools.Opt{})

	srcs := make([]*imagetools.Source, len(res))
	for i, rr := range res {
		desc, err := nodeOCIDescriptor(rr, e.nodeDirs[i])
		if err != nil {
			return ocispecs.Descriptor{}, err
		}
		loc, err := imagetools.ParseLocation("oci-layout://" + e.nodeDirs[i] + "@" + desc.Digest.String())
		if err != nil {
			return ocispecs.Descriptor{}, err
		}
		srcs[i] = &imagetools.Source{Desc: desc, Ref: loc}
	}

	dt, desc, manifests, err := r.Combine(ctx, srcs, ann, false, nil)
	if err != nil {
		return ocispecs.Descriptor{}, err
	}
	if manifests == nil {
		manifests = []imagetools.DescWithSource{{Descriptor: desc, Source: srcs[0]}}
	}

	dir := e.outputDir
	if dir == "" {
		dir = filepath.Join(e.tmpDir, "merged")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ocispecs.Descriptor{}, err
	}
	store, err := contentlocal.NewStore(dir)
	if err != nil {
		return ocispecs.Descriptor{}, err
	}
	dest, err := imagetools.ParseLocation("oci-layout://" + dir + "@" + desc.Digest.String())
	if err != nil {
		return ocispecs.Descriptor{}, err
	}
	for _, m := range manifests {
		if err := r.CopyWithIngester(ctx, &imagetools.Source{Ref: m.Source.Ref, Desc: m.Descriptor}, dest, store); err != nil {
			return ocispecs.Descriptor{}, errors.Wrapf(err, "failed to copy %s from %s", m.Digest, m.Source.Ref)
		}
	}
	if err := content.WriteBlob(ctx, store, desc.Digest.String(), bytes.NewReader(dt), desc); err != nil {
		return ocispecs.Descriptor{}, errors.Wrap(err, "failed to write merged index")
	}

	if e.outputDir != "" {
		names := []ociindex.NameOrTag{ociindex.Tag("latest")}
		if len(e.names) > 0 {
			names = make([]ociindex.NameOrTag, len(e.names))
			for i, n := range e.names {
				names[i] = ociindex.Name(n)
			}
		}
		if err := ociindex.NewStoreIndex(dir).Put(desc, names...); err != nil {
			return ocispecs.Descriptor{}, err
		}
		return desc, nil
	}

	w, err := e.output(nil)
	if err != nil {
		return ocispecs.Descriptor{}, err
	}
	err = imgarchive.Export(ctx, store, w,
		imgarchive.WithManifest(desc, e.names...),
		imgarchive.WithAllPlatforms(),
		imgarchive.WithSkipDockerManifest(),
	)
	if err1 := w.Close(); err == nil {
		err = err1
	}
	if err != nil {
		return ocispecs.Descriptor{}, errors.Wrap(err, "failed to export merged oci archive")
	}
	return desc, nil
}

// nodeOCIDescriptor returns the descriptor of the image exported by a node,
// preferring the descriptor from the exporter response and falling back to
// the index of the node's OCI layout.
func nodeOCIDescriptor(rr *client.SolveResponse, dir string) (ocispecs.Descriptor, error) {
	if rr != nil {
		if s, ok := rr.ExporterResponse[exptypes.ExporterImageDescriptorKey]; ok {
			dt, err := base64.StdEncoding.DecodeString(s)
			if err != nil {
				return ocispecs.Descriptor{}, err
			}
			var desc ocispecs.Descriptor
			if err := json.Unmarshal(dt, &desc); err != nil {
				return ocispecs.Descriptor{}, errors.Wrapf(err, "failed to unmarshal descriptor %s", s)
			}
			return desc, nil
		}
	}
	idx, err := ociindex.NewStoreIndex(dir).Read()
	if err != nil {
		return ocispecs.Descriptor{}, errors.Wrapf(err, "failed to read oci layout index of %s", dir)
	}
	if len(idx.Manifests) == 0 {
		return ocispecs.Descriptor{}, errors.Errorf("no image found in oci layout %s", dir)
	}
	desc := idx.Manifests[0]
	desc.Annotations = nil
	return desc, nil
}
