package build

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"strings"
	"sync"

	"github.com/containerd/containerd/v2/core/content"
	"github.com/containerd/containerd/v2/core/content/proxy"
	"github.com/docker/buildx/util/confutil"
	"github.com/docker/buildx/util/progress"
	controlapi "github.com/moby/buildkit/api/services/control"
	"github.com/moby/buildkit/client"
	provenancetypes "github.com/moby/buildkit/solver/llbsolver/provenance/types"
	digest "github.com/opencontainers/go-digest"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
)

type provenancePredicate struct {
	Builder *provenanceBuilder `json:"builder,omitempty"`
	provenancetypes.ProvenancePredicate
}

type provenanceBuilder struct {
	ID string `json:"id,omitempty"`
}

func setRecordProvenance(ctx context.Context, c *client.Client, sr *client.SolveResponse, ref string, mode confutil.MetadataProvenanceMode, pw progress.Writer) error {
	if mode == confutil.MetadataProvenanceModeDisabled {
		return nil
	}
	pw = progress.ResetTime(pw)
	return progress.Wrap("resolving provenance for metadata file", pw.Write, func(l progress.SubLogger) error {
		res, err := fetchProvenance(ctx, c, ref, mode)
		if err != nil {
			return err
		}
		for k, v := range res {
			sr.ExporterResponse[k] = v
		}
		return nil
	})
}

func fetchProvenance(ctx context.Context, c *client.Client, ref string, mode confutil.MetadataProvenanceMode) (out map[string]string, err error) {
	cl, err := c.ControlClient().ListenBuildHistory(ctx, &controlapi.BuildHistoryRequest{
		Ref:       ref,
		EarlyExit: true,
	})
	if err != nil {
		return nil, err
	}

	var mu sync.Mutex
	eg, ctx := errgroup.WithContext(ctx)
	store := proxy.NewContentStore(c.ContentClient())
	for {
		ev, err := cl.Recv()
		if errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, err
		}
		if ev.Record == nil {
			continue
		}
		if ev.Record.Result != nil {
			desc := lookupProvenance(ev.Record.Result)
			if desc == nil {
				continue
			}
			eg.Go(func() error {
				dt, err := content.ReadBlob(ctx, store, *desc)
				if err != nil {
					return errors.Wrapf(err, "failed to load provenance blob from build record")
				}
				prv, err := encodeProvenance(dt, mode)
				if err != nil {
					return err
				}
				mu.Lock()
				if out == nil {
					out = make(map[string]string)
				}
				out["buildx.build.provenance"] = prv
				mu.Unlock()
				return nil
			})
		} else if ev.Record.Results != nil {
			for platform, res := range ev.Record.Results {
				platform := platform
				desc := lookupProvenance(res)
				if desc == nil {
					continue
				}
				eg.Go(func() error {
					dt, err := content.ReadBlob(ctx, store, *desc)
					if err != nil {
						return errors.Wrapf(err, "failed to load provenance blob from build record")
					}
					prv, err := encodeProvenance(dt, mode)
					if err != nil {
						return err
					}
					mu.Lock()
					if out == nil {
						out = make(map[string]string)
					}
					out["buildx.build.provenance/"+platform] = prv
					mu.Unlock()
					return nil
				})
			}
		}
	}
	return out, eg.Wait()
}

func lookupProvenance(res *controlapi.BuildResultInfo) *ocispecs.Descriptor {
	for _, a := range res.Attestations {
		if a.MediaType == "application/vnd.in-toto+json" && strings.HasPrefix(a.Annotations["in-toto.io/predicate-type"], "https://slsa.dev/provenance/") {
			return &ocispecs.Descriptor{
				Digest:      digest.Digest(a.Digest),
				Size:        a.Size,
				MediaType:   a.MediaType,
				Annotations: a.Annotations,
			}
		}
	}
	return nil
}

func encodeProvenance(dt []byte, mode confutil.MetadataProvenanceMode) (string, error) {
	var prv provenancePredicate
	if err := json.Unmarshal(dt, &prv); err != nil {
		return "", errors.Wrapf(err, "failed to unmarshal provenance")
	}
	if prv.Builder != nil && prv.Builder.ID == "" {
		// reset builder if id is empty
		prv.Builder = nil
	}
	if mode == confutil.MetadataProvenanceModeMin {
		// reset fields for minimal provenance
		prv.BuildConfig = nil
		prv.Metadata = nil
	}
	dtprv, err := json.Marshal(prv)
	if err != nil {
		return "", errors.Wrapf(err, "failed to marshal provenance")
	}
	return base64.StdEncoding.EncodeToString(dtprv), nil
}
