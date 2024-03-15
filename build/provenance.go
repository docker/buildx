package build

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/containerd/containerd/content/proxy"
	"github.com/docker/buildx/util/confutil"
	"github.com/docker/buildx/util/progress"
	controlapi "github.com/moby/buildkit/api/services/control"
	"github.com/moby/buildkit/client"
	provenancetypes "github.com/moby/buildkit/solver/llbsolver/provenance/types"
	"github.com/opencontainers/go-digest"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
)

func setRecordProvenance(ctx context.Context, c *client.Client, sr *client.SolveResponse, ref string, pw progress.Writer) error {
	mode := confutil.MetadataProvenance()
	if mode == confutil.MetadataProvenanceModeNone {
		return nil
	}
	pw = progress.ResetTime(pw)
	return progress.Wrap("resolve build record provenance", pw.Write, func(l progress.SubLogger) error {
		bo := backoff.NewExponentialBackOff()
		bo.MaxElapsedTime = 5 * time.Second
		return backoff.Retry(func() error {
			res, err := fetchProvenance(ctx, c, ref, mode, l)
			if err != nil {
				return err
			}
			for k, v := range res {
				sr.ExporterResponse[k] = v
			}
			return nil
		}, bo)
	})
}

func fetchProvenance(ctx context.Context, c *client.Client, ref string, mode confutil.MetadataProvenanceMode, l progress.SubLogger) (out map[string]string, err error) {
	cl, err := c.ControlClient().ListenBuildHistory(ctx, &controlapi.BuildHistoryRequest{
		Ref:       ref,
		EarlyExit: true,
	})
	if err != nil {
		return nil, err
	}

	var mu sync.Mutex
	eg, ctx := errgroup.WithContext(ctx)
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
			provenanceDgst := provenanceDigest(ev.Record.Result)
			if provenanceDgst == nil {
				continue
			}
			eg.Go(func() error {
				dt, err := getBlob(ctx, c, *provenanceDgst, "", l)
				if err != nil {
					return errors.Wrapf(err, "failed to load provenance from build record")
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
				provenanceDgst := provenanceDigest(res)
				if provenanceDgst == nil {
					continue
				}
				eg.Go(func() error {
					dt, err := getBlob(ctx, c, *provenanceDgst, platform, l)
					if err != nil {
						return errors.Wrapf(err, "failed to load provenance from build record")
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

func provenanceDigest(res *controlapi.BuildResultInfo) *digest.Digest {
	for _, a := range res.Attestations {
		if a.MediaType == "application/vnd.in-toto+json" && strings.HasPrefix(a.Annotations["in-toto.io/predicate-type"], "https://slsa.dev/provenance/") {
			return &a.Digest
		}
	}
	return nil
}

func encodeProvenance(dt []byte, mode confutil.MetadataProvenanceMode) (string, error) {
	if mode == confutil.MetadataProvenanceModeMax {
		return base64.StdEncoding.EncodeToString(dt), nil
	}

	var prv provenancetypes.ProvenancePredicate
	if err := json.Unmarshal(dt, &prv); err != nil {
		return "", errors.Wrapf(err, "failed to unmarshal provenance")
	}

	// remove buildConfig and metadata for minimal provenance
	prv.BuildConfig = nil
	prv.Metadata = nil

	dtmin, err := json.Marshal(prv)
	if err != nil {
		return "", errors.Wrapf(err, "failed to marshal minimal provenance")
	}

	return base64.StdEncoding.EncodeToString(dtmin), nil
}

func getBlob(ctx context.Context, c *client.Client, dgst digest.Digest, platform string, l progress.SubLogger) (dt []byte, err error) {
	id := "fetching " + dgst.String()
	if platform != "" {
		id = fmt.Sprintf("[%s] fetching %s", platform, dgst.String())
	}
	st := &client.VertexStatus{
		ID: id,
	}

	defer func() {
		now := time.Now()
		st.Completed = &now
		if err == nil {
			st.Total = st.Current
		}
		l.SetStatus(st)
	}()

	now := time.Now()
	st.Started = &now
	l.SetStatus(st)

	store := proxy.NewContentStore(c.ContentClient())
	ra, err := store.ReaderAt(ctx, ocispecs.Descriptor{
		Digest: dgst,
	})
	if err != nil {
		return nil, err
	}
	defer ra.Close()

	for {
		buf := make([]byte, 1024)
		n, err := ra.ReadAt(buf, st.Current)
		if err != nil && err != io.EOF {
			return nil, err
		}
		dt = append(dt, buf[:n]...)
		st.Current += int64(n)
		l.SetStatus(st)
		if err == io.EOF {
			break
		}
	}

	return dt, nil
}
