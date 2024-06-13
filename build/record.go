package build

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"strings"
	"sync"

	"github.com/containerd/containerd/content"
	"github.com/containerd/containerd/content/proxy"
	"github.com/docker/buildx/util/confutil"
	"github.com/docker/buildx/util/progress"
	controlapi "github.com/moby/buildkit/api/services/control"
	"github.com/moby/buildkit/client"
	provenancetypes "github.com/moby/buildkit/solver/llbsolver/provenance/types"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
)

const (
	refMetadataBuildxProvenance = "buildx.build.provenance"
	refMetadataBuildxStatus     = "buildx.build.status"
)

func setRecordMetadata(ctx context.Context, c *client.Client, sr *client.SolveResponse, ref string, pw progress.Writer) error {
	var mu sync.Mutex

	cb := func(key string, value string) {
		mu.Lock()
		defer mu.Unlock()
		sr.ExporterResponse[key] = value
	}

	eg, ctx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		return setRecordProvenance(ctx, c, ref, cb, pw)
	})
	eg.Go(func() error {
		return setRecordStatus(ctx, c, ref, cb, pw)
	})

	return eg.Wait()
}

type provenancePredicate struct {
	Builder *provenanceBuilder `json:"builder,omitempty"`
	provenancetypes.ProvenancePredicate
}

type provenanceBuilder struct {
	ID string `json:"id,omitempty"`
}

func setRecordProvenance(ctx context.Context, c *client.Client, ref string, updateExporterResponse func(key string, value string), pw progress.Writer) error {
	mode := confutil.MetadataProvenance()
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
			updateExporterResponse(k, v)
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
				out[refMetadataBuildxProvenance] = prv
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
					out[refMetadataBuildxProvenance+"/"+platform] = prv
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
				Digest:      a.Digest,
				Size:        a.Size_,
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

func setRecordStatus(ctx context.Context, c *client.Client, ref string, updateExporterResponse func(key string, value string), pw progress.Writer) error {
	mode := confutil.MetadataStatus()
	if mode == confutil.MetadataStatusModeDisabled {
		return nil
	}
	pw = progress.ResetTime(pw)
	return progress.Wrap("resolving status for metadata file", pw.Write, func(l progress.SubLogger) error {
		status, err := fetchStatus(ctx, c, ref)
		if err != nil {
			return err
		} else if status == nil {
			return nil
		}

		if mode == confutil.MetadataStatusModeWarnings {
			if len(status.Warnings) == 0 {
				return nil
			}
			// we just want warnings
			status.Vertexes = nil
			status.Statuses = nil
			status.Logs = nil
		}

		dt, err := json.Marshal(status)
		if err != nil {
			return errors.Wrapf(err, "failed to marshal status")
		}

		updateExporterResponse(refMetadataBuildxStatus, base64.StdEncoding.EncodeToString(dt))
		return nil
	})
}

func fetchStatus(ctx context.Context, c *client.Client, ref string) (*client.SolveStatus, error) {
	cl, err := c.ControlClient().Status(ctx, &controlapi.StatusRequest{
		Ref: ref,
	})
	if err != nil {
		return nil, err
	}
	resp, err := cl.Recv()
	if err != nil {
		if err == io.EOF {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to receive status")
	}
	return client.NewSolveStatus(resp), nil
}
