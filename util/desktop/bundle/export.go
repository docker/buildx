package bundle

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"

	"github.com/containerd/containerd/v2/core/content"
	"github.com/containerd/containerd/v2/core/content/proxy"
	imgarchive "github.com/containerd/containerd/v2/core/images/archive"
	"github.com/docker/buildx/localstate"
	controlapi "github.com/moby/buildkit/api/services/control"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/util/contentutil"
	"github.com/opencontainers/go-digest"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
)

const (
	HistoryRecordMediaTypeV0 = "application/vnd.buildkit.historyrecord.v0"
	RefDescriptorMediaType   = "vnd.export-build.descriptor.mediatype"
)

type Record struct {
	*controlapi.BuildHistoryRecord

	DefaultPlatform string
	LocalState      *localstate.State      `json:"localState,omitempty"`
	StateGroup      *localstate.StateGroup `json:"stateGroup,omitempty"`
}

func Export(ctx context.Context, c *client.Client, w io.Writer, records []*Record) error {
	store := proxy.NewContentStore(c.ContentClient())
	mp := contentutil.NewMultiProvider(store)

	desc, err := export(ctx, mp, records)
	if err != nil {
		return errors.Wrap(err, "failed to export")
	}

	gz := gzip.NewWriter(w)
	defer gz.Close()

	if err := imgarchive.Export(ctx, mp, gz, imgarchive.WithManifest(desc), imgarchive.WithSkipDockerManifest()); err != nil {
		return errors.Wrap(err, "failed to create dockerbuild archive")
	}

	return nil
}

func export(ctx context.Context, mp *contentutil.MultiProvider, records []*Record) (ocispecs.Descriptor, error) {
	if len(records) == 1 {
		desc, err := exportRecord(ctx, mp, records[0])
		if err != nil {
			return ocispecs.Descriptor{}, errors.Wrap(err, "failed to export record")
		}
		return desc, nil
	}

	var idx ocispecs.Index
	idx.MediaType = ocispecs.MediaTypeImageIndex
	idx.SchemaVersion = 2

	for _, r := range records {
		desc, err := exportRecord(ctx, mp, r)
		if err != nil {
			return ocispecs.Descriptor{}, errors.Wrap(err, "failed to export record")
		}
		if desc.Annotations == nil {
			desc.Annotations = make(map[string]string)
		}
		desc.Annotations["vnd.buildkit.history.reference"] = r.Ref
		idx.Manifests = append(idx.Manifests, desc)
	}

	desc, err := writeJSON(ctx, mp, idx.MediaType, idx)
	if err != nil {
		return ocispecs.Descriptor{}, errors.Wrap(err, "failed to write index")
	}

	return desc, nil
}

func writeJSON(ctx context.Context, mp *contentutil.MultiProvider, mt string, data any) (ocispecs.Descriptor, error) {
	dt, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return ocispecs.Descriptor{}, errors.Wrap(err, "failed to marshal data")
	}

	desc := ocispecs.Descriptor{
		MediaType: mt,
		Size:      int64(len(dt)),
		Digest:    digest.FromBytes(dt),
	}

	buf := contentutil.NewBuffer()
	if err := content.WriteBlob(ctx, buf, "blob-"+desc.Digest.String(), bytes.NewReader(dt), desc); err != nil {
		return ocispecs.Descriptor{}, errors.Wrap(err, "failed to write blob")
	}

	mp.Add(desc.Digest, buf)
	return desc, nil
}

func sanitizeCacheImports(v string) (string, error) {
	type cacheImport struct {
		Type  string            `json:"Type"`
		Attrs map[string]string `json:"Attrs"`
	}
	var arr []cacheImport
	if err := json.Unmarshal([]byte(v), &arr); err != nil {
		return "", errors.Wrap(err, "failed to unmarshal cache imports")
	}
	for i := range arr {
		m := map[string]string{}
		for k, v := range arr[i].Attrs {
			if k == "scope" || k == "ref" {
				m[k] = v
			}
		}
		arr[i].Attrs = m
	}
	dt, err := json.Marshal(arr)
	if err != nil {
		return "", errors.Wrap(err, "failed to marshal cache imports")
	}
	return string(dt), nil
}

func sanitizeRecord(rec *controlapi.BuildHistoryRecord) {
	for k, v := range rec.FrontendAttrs {
		if k == "cache-imports" {
			v, err := sanitizeCacheImports(v)
			if err != nil {
				rec.FrontendAttrs[k] = ""
			} else {
				rec.FrontendAttrs[k] = v
			}
		}
	}
}

func exportRecord(ctx context.Context, mp *contentutil.MultiProvider, record *Record) (ocispecs.Descriptor, error) {
	var mfst ocispecs.Manifest
	mfst.MediaType = ocispecs.MediaTypeImageManifest
	mfst.SchemaVersion = 2

	sanitizeRecord(record.BuildHistoryRecord)

	visited := map[string]struct{}{}

	if trace := record.Trace; trace != nil {
		desc, err := loadDescriptor(ctx, mp, trace, visited)
		if err != nil {
			return ocispecs.Descriptor{}, errors.Wrap(err, "failed to load trace descriptor")
		}
		desc, err = sanitizeTrace(ctx, mp, *desc)
		if err != nil {
			return ocispecs.Descriptor{}, errors.Wrap(err, "failed to sanitize trace")
		}
		record.Trace.Digest = desc.Digest.String()
		record.Trace.Size = desc.Size
		mfst.Layers = append(mfst.Layers, *desc)
	}

	config, err := writeJSON(ctx, mp, HistoryRecordMediaTypeV0, record)
	if err != nil {
		return ocispecs.Descriptor{}, errors.Wrap(err, "failed to write history record")
	}

	mfst.Config = config

	if logs := record.Logs; logs != nil {
		desc, err := loadDescriptor(ctx, mp, logs, visited)
		if err != nil {
			return ocispecs.Descriptor{}, errors.Wrap(err, "failed to load logs descriptor")
		}
		mfst.Layers = append(mfst.Layers, *desc)
	}

	if res := record.Result; res != nil {
		results, err := loadResult(ctx, mp, res, visited)
		if err != nil {
			return ocispecs.Descriptor{}, errors.Wrap(err, "failed to load result")
		}
		mfst.Layers = append(mfst.Layers, results...)
	}

	if exterr := record.ExternalError; exterr != nil {
		desc, err := loadDescriptor(ctx, mp, exterr, visited)
		if err != nil {
			return ocispecs.Descriptor{}, errors.Wrap(err, "failed to load external error descriptor")
		}
		mfst.Layers = append(mfst.Layers, *desc)
	}

	for _, res := range record.Results {
		results, err := loadResult(ctx, mp, res, visited)
		if err != nil {
			return ocispecs.Descriptor{}, errors.Wrap(err, "failed to load result")
		}
		mfst.Layers = append(mfst.Layers, results...)
	}

	desc, err := writeJSON(ctx, mp, mfst.MediaType, mfst)
	if err != nil {
		return ocispecs.Descriptor{}, errors.Wrap(err, "failed to write manifest")
	}

	return desc, nil
}

func loadResult(ctx context.Context, ip content.InfoProvider, in *controlapi.BuildResultInfo, visited map[string]struct{}) ([]ocispecs.Descriptor, error) {
	var out []ocispecs.Descriptor
	for _, attest := range in.Attestations {
		desc, err := loadDescriptor(ctx, ip, attest, visited)
		if err != nil {
			return nil, errors.Wrap(err, "failed to load attestation descriptor")
		}
		if desc != nil {
			out = append(out, *desc)
		}
	}
	for _, r := range in.Results {
		desc, err := loadDescriptor(ctx, ip, r, visited)
		if err != nil {
			return nil, errors.Wrap(err, "failed to load result descriptor")
		}
		if desc != nil {
			if desc.Annotations == nil {
				desc.Annotations = make(map[string]string)
			}
			// Override media type to avoid containerd to walk children. Also
			// keep original media type in annotations.
			desc.Annotations[RefDescriptorMediaType] = desc.MediaType
			desc.MediaType = "application/json"
			out = append(out, *desc)
		}
	}
	return out, nil
}

func loadDescriptor(ctx context.Context, ip content.InfoProvider, in *controlapi.Descriptor, visited map[string]struct{}) (*ocispecs.Descriptor, error) {
	if _, ok := visited[in.Digest]; ok {
		return nil, nil
	}
	visited[in.Digest] = struct{}{}

	dgst, err := digest.Parse(in.Digest)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse digest")
	}

	if _, err := ip.Info(ctx, dgst); err != nil {
		return nil, errors.Wrap(err, "failed to get info")
	}

	return &ocispecs.Descriptor{
		MediaType:   in.MediaType,
		Digest:      dgst,
		Size:        in.Size,
		Annotations: in.Annotations,
	}, nil
}
