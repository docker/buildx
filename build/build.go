package build

import (
	"bytes"
	"context"
	_ "crypto/sha256" // ensure digests can be computed
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/containerd/containerd/v2/core/images"
	"github.com/distribution/reference"
	"github.com/docker/buildx/builder"
	controllerapi "github.com/docker/buildx/controller/pb"
	"github.com/docker/buildx/driver"
	"github.com/docker/buildx/util/confutil"
	"github.com/docker/buildx/util/desktop"
	"github.com/docker/buildx/util/dockerutil"
	"github.com/docker/buildx/util/imagetools"
	"github.com/docker/buildx/util/progress"
	"github.com/docker/buildx/util/resolver"
	"github.com/docker/buildx/util/waitmap"
	"github.com/docker/cli/opts"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/pkg/jsonmessage"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/exporter/containerimage/exptypes"
	gateway "github.com/moby/buildkit/frontend/gateway/client"
	"github.com/moby/buildkit/identity"
	"github.com/moby/buildkit/session"
	"github.com/moby/buildkit/session/filesync"
	"github.com/moby/buildkit/solver/errdefs"
	"github.com/moby/buildkit/solver/pb"
	spb "github.com/moby/buildkit/sourcepolicy/pb"
	"github.com/moby/buildkit/util/entitlements"
	"github.com/moby/buildkit/util/progress/progresswriter"
	"github.com/moby/buildkit/util/tracing"
	"github.com/opencontainers/go-digest"
	specs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/tonistiigi/fsutil"
	fstypes "github.com/tonistiigi/fsutil/types"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"
	"google.golang.org/protobuf/proto"
)

const (
	printFallbackImage     = "docker/dockerfile:1.7.1@sha256:a57df69d0ea827fb7266491f2813635de6f17269be881f696fbfdf2d83dda33e"
	printLintFallbackImage = "docker/dockerfile:1.8.1@sha256:e87caa74dcb7d46cd820352bfea12591f3dba3ddc4285e19c7dcd13359f7cefd"
)

type Options struct {
	Inputs Inputs

	Ref                        string
	Allow                      []entitlements.Entitlement
	Attests                    map[string]*string
	BuildArgs                  map[string]string
	CacheFrom                  []client.CacheOptionsEntry
	CacheTo                    []client.CacheOptionsEntry
	CgroupParent               string
	Exports                    []client.ExportEntry
	ExportsLocalPathsTemporary []string // should be removed after client.ExportEntry update in buildkit v0.19.0
	ExtraHosts                 []string
	Labels                     map[string]string
	NetworkMode                string
	NoCache                    bool
	NoCacheFilter              []string
	Platforms                  []specs.Platform
	Pull                       bool
	SecretSpecs                []*controllerapi.Secret
	SSHSpecs                   []*controllerapi.SSH
	ShmSize                    opts.MemBytes
	Tags                       []string
	Target                     string
	Ulimits                    *opts.UlimitOpt

	Session                []session.Attachable
	Linked                 bool // Linked marks this target as exclusively linked (not requested by the user).
	CallFunc               *CallFunc
	ProvenanceResponseMode confutil.MetadataProvenanceMode
	SourcePolicy           *spb.Policy
	GroupRef               string
}

type CallFunc struct {
	Name         string
	Format       string
	IgnoreStatus bool
}

type Inputs struct {
	ContextPath      string
	DockerfilePath   string
	InStream         *SyncMultiReader
	ContextState     *llb.State
	DockerfileInline string
	NamedContexts    map[string]NamedContext
	// DockerfileMappingSrc and DockerfileMappingDst are filled in by the builder.
	DockerfileMappingSrc string
	DockerfileMappingDst string
}

type NamedContext struct {
	Path  string
	State *llb.State
}

type reqForNode struct {
	*resolvedNode
	so *client.SolveOpt
}

func filterAvailableNodes(nodes []builder.Node) ([]builder.Node, error) {
	out := make([]builder.Node, 0, len(nodes))
	err := errors.Errorf("no drivers found")
	for _, n := range nodes {
		if n.Err == nil && n.Driver != nil {
			out = append(out, n)
		}
		if n.Err != nil {
			err = n.Err
		}
	}
	if len(out) > 0 {
		return out, nil
	}
	return nil, err
}

func toRepoOnly(in string) (string, error) {
	m := map[string]struct{}{}
	p := strings.Split(in, ",")
	for _, pp := range p {
		n, err := reference.ParseNormalizedNamed(pp)
		if err != nil {
			return "", err
		}
		m[n.Name()] = struct{}{}
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return strings.Join(out, ","), nil
}

func Build(ctx context.Context, nodes []builder.Node, opts map[string]Options, docker *dockerutil.Client, cfg *confutil.Config, w progress.Writer) (resp map[string]*client.SolveResponse, err error) {
	return BuildWithResultHandler(ctx, nodes, opts, docker, cfg, w, nil)
}

func BuildWithResultHandler(ctx context.Context, nodes []builder.Node, opts map[string]Options, docker *dockerutil.Client, cfg *confutil.Config, w progress.Writer, resultHandleFunc func(driverIndex int, rCtx *ResultHandle)) (resp map[string]*client.SolveResponse, err error) {
	if len(nodes) == 0 {
		return nil, errors.Errorf("driver required for build")
	}

	nodes, err = filterAvailableNodes(nodes)
	if err != nil {
		return nil, errors.Wrapf(err, "no valid drivers found")
	}

	var noMobyDriver *driver.DriverHandle
	for _, n := range nodes {
		if !n.Driver.IsMobyDriver() {
			noMobyDriver = n.Driver
			break
		}
	}

	if noMobyDriver != nil && !noDefaultLoad() && noCallFunc(opts) {
		var noOutputTargets []string
		for name, opt := range opts {
			if noMobyDriver.Features(ctx)[driver.DefaultLoad] {
				continue
			}

			if !opt.Linked && len(opt.Exports) == 0 {
				noOutputTargets = append(noOutputTargets, name)
			}
		}
		if len(noOutputTargets) > 0 {
			var warnNoOutputBuf bytes.Buffer
			warnNoOutputBuf.WriteString("No output specified ")
			if len(noOutputTargets) == 1 && noOutputTargets[0] == "default" {
				warnNoOutputBuf.WriteString(fmt.Sprintf("with %s driver", noMobyDriver.Factory().Name()))
			} else {
				warnNoOutputBuf.WriteString(fmt.Sprintf("for %s target(s) with %s driver", strings.Join(noOutputTargets, ", "), noMobyDriver.Factory().Name()))
			}
			logrus.Warnf("%s. Build result will only remain in the build cache. To push result image into registry use --push or to load image into docker use --load", warnNoOutputBuf.String())
		}
	}

	drivers, err := resolveDrivers(ctx, nodes, opts, w)
	if err != nil {
		return nil, err
	}

	defers := make([]func(), 0, 2)
	defer func() {
		if err != nil {
			for _, f := range defers {
				f()
			}
		}
	}()

	reqForNodes := make(map[string][]*reqForNode)
	eg, ctx := errgroup.WithContext(ctx)

	for k, opt := range opts {
		multiDriver := len(drivers[k]) > 1
		hasMobyDriver := false
		addGitAttrs, err := getGitAttributes(ctx, opt.Inputs.ContextPath, opt.Inputs.DockerfilePath)
		if err != nil {
			logrus.WithError(err).Warn("current commit information was not captured by the build")
		}
		if opt.Ref == "" {
			opt.Ref = identity.NewID()
		}
		var reqn []*reqForNode
		for _, np := range drivers[k] {
			if np.Node().Driver.IsMobyDriver() {
				hasMobyDriver = true
			}
			opt.Platforms = np.platforms
			gatewayOpts, err := np.BuildOpts(ctx)
			if err != nil {
				return nil, err
			}
			localOpt := opt
			so, release, err := toSolveOpt(ctx, np.Node(), multiDriver, &localOpt, gatewayOpts, cfg, w, docker)
			opts[k] = localOpt
			if err != nil {
				return nil, err
			}
			if err := saveLocalState(so, k, opt, np.Node(), cfg); err != nil {
				return nil, err
			}
			addGitAttrs(so)
			defers = append(defers, release)
			reqn = append(reqn, &reqForNode{
				resolvedNode: np,
				so:           so,
			})
		}
		reqForNodes[k] = reqn
		for _, at := range opt.Session {
			if s, ok := at.(interface {
				SetLogger(progresswriter.Logger)
			}); ok {
				s.SetLogger(func(s *client.SolveStatus) {
					w.Write(s)
				})
			}
		}

		// validate for multi-node push
		if hasMobyDriver && multiDriver {
			for _, np := range reqForNodes[k] {
				for _, e := range np.so.Exports {
					if e.Type == "moby" {
						if ok, _ := strconv.ParseBool(e.Attrs["push"]); ok {
							return nil, errors.Errorf("multi-node push can't currently be performed with the docker driver, please switch to a different driver")
						}
					}
				}
			}
		}
	}

	// validate that all links between targets use same drivers
	for name := range opts {
		dps := reqForNodes[name]
		for i, dp := range dps {
			so := reqForNodes[name][i].so
			for k, v := range so.FrontendAttrs {
				if strings.HasPrefix(k, "context:") && strings.HasPrefix(v, "target:") {
					k2 := strings.TrimPrefix(v, "target:")
					dps2, ok := drivers[k2]
					if !ok {
						return nil, errors.Errorf("failed to find target %s for context %s", k2, strings.TrimPrefix(k, "context:")) // should be validated before already
					}
					var found bool
					for _, dp2 := range dps2 {
						if dp2.driverIndex == dp.driverIndex {
							found = true
							break
						}
					}
					if !found {
						return nil, errors.Errorf("failed to use %s as context %s for %s because targets build with different drivers", k2, strings.TrimPrefix(k, "context:"), name)
					}
				}
			}
		}
	}

	sharedSessions, err := detectSharedMounts(ctx, reqForNodes)
	if err != nil {
		return nil, err
	}
	sharedSessionsWG := map[string]*sync.WaitGroup{}

	resp = map[string]*client.SolveResponse{}
	var respMu sync.Mutex
	results := waitmap.New()

	multiTarget := len(opts) > 1
	childTargets := calculateChildTargets(reqForNodes, opts)

	for k, opt := range opts {
		err := func(k string) (err error) {
			opt := opt
			dps := drivers[k]
			multiDriver := len(drivers[k]) > 1

			var span trace.Span
			ctx := ctx
			if multiTarget {
				span, ctx = tracing.StartSpan(ctx, k)
			}
			baseCtx := ctx

			if multiTarget {
				defer func() {
					err = errors.Wrapf(err, "target %s", k)
				}()
			}

			res := make([]*client.SolveResponse, len(dps))
			eg2, ctx := errgroup.WithContext(ctx)

			var pushNames string
			var insecurePush bool

			for i, dp := range dps {
				i, dp := i, dp
				node := dp.Node()
				so := reqForNodes[k][i].so
				if multiDriver {
					for i, e := range so.Exports {
						switch e.Type {
						case "oci", "tar":
							return errors.Errorf("%s for multi-node builds currently not supported", e.Type)
						case "image":
							if pushNames == "" && e.Attrs["push"] != "" {
								if ok, _ := strconv.ParseBool(e.Attrs["push"]); ok {
									pushNames = e.Attrs["name"]
									if pushNames == "" {
										return errors.Errorf("tag is needed when pushing to registry")
									}
									names, err := toRepoOnly(e.Attrs["name"])
									if err != nil {
										return err
									}
									if ok, _ := strconv.ParseBool(e.Attrs["registry.insecure"]); ok {
										insecurePush = true
									}
									e.Attrs["name"] = names
									e.Attrs["push-by-digest"] = "true"
									so.Exports[i].Attrs = e.Attrs
								}
							}
						}
					}
				}

				pw := progress.WithPrefix(w, k, multiTarget)

				c, err := dp.Client(ctx)
				if err != nil {
					return err
				}

				var done func()
				if sessions, ok := sharedSessions[node.Name]; ok {
					wg, ok := sharedSessionsWG[node.Name]
					if ok {
						wg.Add(1)
					} else {
						wg = &sync.WaitGroup{}
						wg.Add(1)
						sharedSessionsWG[node.Name] = wg
						for _, s := range sessions {
							s := s
							eg.Go(func() error {
								return s.Run(baseCtx, c.Dialer())
							})
						}
						go func() {
							wg.Wait()
							for _, s := range sessions {
								s.Close()
							}
						}()
					}
					done = wg.Done
				}

				eg2.Go(func() error {
					if done != nil {
						defer done()
					}

					pw = progress.ResetTime(pw)

					if err := waitContextDeps(ctx, dp.driverIndex, results, so); err != nil {
						return err
					}

					frontendInputs := make(map[string]*pb.Definition)
					for key, st := range so.FrontendInputs {
						def, err := st.Marshal(ctx)
						if err != nil {
							return err
						}
						frontendInputs[key] = def.ToPB()
					}

					req := gateway.SolveRequest{
						Frontend:       so.Frontend,
						FrontendInputs: frontendInputs,
						FrontendOpt:    make(map[string]string),
					}
					for k, v := range so.FrontendAttrs {
						req.FrontendOpt[k] = v
					}
					so.Frontend = ""
					so.FrontendInputs = nil

					ch, done := progress.NewChannel(pw)
					defer func() { <-done }()

					cc := c
					var callRes map[string][]byte
					buildFunc := func(ctx context.Context, c gateway.Client) (*gateway.Result, error) {
						if opt.CallFunc != nil {
							if _, ok := req.FrontendOpt["frontend.caps"]; !ok {
								req.FrontendOpt["frontend.caps"] = "moby.buildkit.frontend.subrequests+forward"
							} else {
								req.FrontendOpt["frontend.caps"] += ",moby.buildkit.frontend.subrequests+forward"
							}
							req.FrontendOpt["requestid"] = "frontend." + opt.CallFunc.Name
						}

						res, err := c.Solve(ctx, req)
						if err != nil {
							req, ok := fallbackPrintError(err, req)
							if ok {
								res2, err2 := c.Solve(ctx, req)
								if err2 != nil {
									return nil, err
								}
								res = res2
							} else {
								return nil, err
							}
						}
						if opt.CallFunc != nil {
							callRes = res.Metadata
						}

						rKey := resultKey(dp.driverIndex, k)
						results.Set(rKey, res)

						if children, ok := childTargets[rKey]; ok && len(children) > 0 {
							// wait for the child targets to register their LLB before evaluating
							_, err := results.Get(ctx, children...)
							if err != nil {
								return nil, err
							}
							// we need to wait until the child targets have completed before we can release
							eg, ctx := errgroup.WithContext(ctx)
							eg.Go(func() error {
								return res.EachRef(func(ref gateway.Reference) error {
									return ref.Evaluate(ctx)
								})
							})
							eg.Go(func() error {
								_, err := results.Get(ctx, children...)
								return err
							})
							if err := eg.Wait(); err != nil {
								return nil, err
							}
						}

						return res, nil
					}
					buildRef := fmt.Sprintf("%s/%s/%s", node.Builder, node.Name, so.Ref)
					var rr *client.SolveResponse
					if resultHandleFunc != nil {
						var resultHandle *ResultHandle
						resultHandle, rr, err = NewResultHandle(ctx, cc, *so, "buildx", buildFunc, ch)
						resultHandleFunc(dp.driverIndex, resultHandle)
					} else {
						span, ctx := tracing.StartSpan(ctx, "build")
						rr, err = c.Build(ctx, *so, "buildx", buildFunc, ch)
						tracing.FinishWithError(span, err)
					}
					if !so.Internal && desktop.BuildBackendEnabled() && node.Driver.HistoryAPISupported(ctx) {
						if err != nil {
							return &desktop.ErrorWithBuildRef{
								Ref: buildRef,
								Err: err,
							}
						}
						progress.WriteBuildRef(w, k, buildRef)
					}
					if err != nil {
						return err
					}
					res[i] = rr

					if rr.ExporterResponse == nil {
						rr.ExporterResponse = map[string]string{}
					}
					for k, v := range callRes {
						rr.ExporterResponse[k] = string(v)
					}
					if opt.CallFunc == nil {
						rr.ExporterResponse["buildx.build.ref"] = buildRef
						if node.Driver.HistoryAPISupported(ctx) {
							if err := setRecordProvenance(ctx, c, rr, so.Ref, opt.ProvenanceResponseMode, pw); err != nil {
								return err
							}
						}
					}

					node := dp.Node().Driver
					if node.IsMobyDriver() {
						for _, e := range so.Exports {
							if e.Type == "moby" && e.Attrs["push"] != "" {
								if ok, _ := strconv.ParseBool(e.Attrs["push"]); ok {
									pushNames = e.Attrs["name"]
									if pushNames == "" {
										return errors.Errorf("tag is needed when pushing to registry")
									}
									pw := progress.ResetTime(pw)
									pushList := strings.Split(pushNames, ",")
									for _, name := range pushList {
										if err := progress.Wrap(fmt.Sprintf("pushing %s with docker", name), pw.Write, func(l progress.SubLogger) error {
											return pushWithMoby(ctx, node, name, l)
										}); err != nil {
											return err
										}
									}
									remoteDigest, err := remoteDigestWithMoby(ctx, node, pushList[0])
									if err == nil && remoteDigest != "" {
										// old daemons might not have containerimage.config.digest set
										// in response so use containerimage.digest value for it if available
										if _, ok := rr.ExporterResponse[exptypes.ExporterImageConfigDigestKey]; !ok {
											if v, ok := rr.ExporterResponse[exptypes.ExporterImageDigestKey]; ok {
												rr.ExporterResponse[exptypes.ExporterImageConfigDigestKey] = v
											}
										}
										rr.ExporterResponse[exptypes.ExporterImageDigestKey] = remoteDigest
									} else if err != nil {
										return err
									}
								}
							}
						}
					}
					return nil
				})
			}

			eg.Go(func() (err error) {
				ctx := baseCtx
				defer func() {
					if span != nil {
						tracing.FinishWithError(span, err)
					}
				}()

				if multiTarget {
					defer func() {
						err = errors.Wrapf(err, "target %s", k)
					}()
				}

				pw := progress.WithPrefix(w, "default", false)
				if err := eg2.Wait(); err != nil {
					return err
				}

				respMu.Lock()
				resp[k] = res[0]
				respMu.Unlock()
				if len(res) == 1 {
					return nil
				}

				if pushNames != "" {
					err := progress.Write(pw, fmt.Sprintf("merging manifest list %s", pushNames), func() error {
						descs := make([]specs.Descriptor, 0, len(res))

						for _, r := range res {
							s, ok := r.ExporterResponse[exptypes.ExporterImageDescriptorKey]
							if ok {
								dt, err := base64.StdEncoding.DecodeString(s)
								if err != nil {
									return err
								}
								var desc specs.Descriptor
								if err := json.Unmarshal(dt, &desc); err != nil {
									return errors.Wrapf(err, "failed to unmarshal descriptor %s", s)
								}
								descs = append(descs, desc)
								continue
							}
							// This is fallback for some very old buildkit versions.
							// Note that the mediatype isn't really correct as most of the time it is image manifest and
							// not manifest list but actually both are handled because for Docker mediatypes the
							// mediatype value in the Accpet header does not seem to matter.
							s, ok = r.ExporterResponse[exptypes.ExporterImageDigestKey]
							if ok {
								descs = append(descs, specs.Descriptor{
									Digest:    digest.Digest(s),
									MediaType: images.MediaTypeDockerSchema2ManifestList,
									Size:      -1,
								})
							}
						}
						if len(descs) > 0 {
							var imageopt imagetools.Opt
							for _, dp := range dps {
								imageopt = dp.Node().ImageOpt
								break
							}
							names := strings.Split(pushNames, ",")

							if insecurePush {
								insecureTrue := true
								httpTrue := true
								nn, err := reference.ParseNormalizedNamed(names[0])
								if err != nil {
									return err
								}
								imageopt.RegistryConfig = map[string]resolver.RegistryConfig{
									reference.Domain(nn): {
										Insecure:  &insecureTrue,
										PlainHTTP: &httpTrue,
									},
								}
							}

							itpull := imagetools.New(imageopt)

							ref, err := reference.ParseNormalizedNamed(names[0])
							if err != nil {
								return err
							}
							ref = reference.TagNameOnly(ref)

							srcs := make([]*imagetools.Source, len(descs))
							for i, desc := range descs {
								srcs[i] = &imagetools.Source{
									Desc: desc,
									Ref:  ref,
								}
							}

							indexAnnotations, err := extractIndexAnnotations(opt.Exports)
							if err != nil {
								return err
							}

							dt, desc, err := itpull.Combine(ctx, srcs, indexAnnotations, false)
							if err != nil {
								return err
							}

							itpush := imagetools.New(imageopt)

							for _, n := range names {
								nn, err := reference.ParseNormalizedNamed(n)
								if err != nil {
									return err
								}
								if err := itpush.Push(ctx, nn, desc, dt); err != nil {
									return err
								}
							}

							respMu.Lock()
							resp[k] = &client.SolveResponse{
								ExporterResponse: map[string]string{
									exptypes.ExporterImageDigestKey: desc.Digest.String(),
								},
							}
							respMu.Unlock()
						}
						return nil
					})
					if err != nil {
						return err
					}
				}
				return nil
			})

			return nil
		}(k)
		if err != nil {
			return nil, err
		}
	}

	if err := eg.Wait(); err != nil {
		return nil, err
	}

	return resp, nil
}

func extractIndexAnnotations(exports []client.ExportEntry) (map[exptypes.AnnotationKey]string, error) {
	annotations := map[exptypes.AnnotationKey]string{}
	for _, exp := range exports {
		for k, v := range exp.Attrs {
			ak, ok, err := exptypes.ParseAnnotationKey(k)
			if !ok {
				continue
			}
			if err != nil {
				return nil, err
			}

			switch ak.Type {
			case exptypes.AnnotationIndex, exptypes.AnnotationManifestDescriptor:
				annotations[ak] = v
			}
		}
	}
	return annotations, nil
}

func pushWithMoby(ctx context.Context, d *driver.DriverHandle, name string, l progress.SubLogger) error {
	api := d.Config().DockerAPI
	if api == nil {
		return errors.Errorf("invalid empty Docker API reference") // should never happen
	}
	creds, err := imagetools.RegistryAuthForRef(name, d.Config().Auth)
	if err != nil {
		return err
	}

	rc, err := api.ImagePush(ctx, name, image.PushOptions{
		RegistryAuth: creds,
	})
	if err != nil {
		return err
	}

	started := map[string]*client.VertexStatus{}

	defer func() {
		for _, st := range started {
			if st.Completed == nil {
				now := time.Now()
				st.Completed = &now
				l.SetStatus(st)
			}
		}
	}()

	dec := json.NewDecoder(rc)
	var parsedError error
	for {
		var jm jsonmessage.JSONMessage
		if err := dec.Decode(&jm); err != nil {
			if parsedError != nil {
				return parsedError
			}
			if err == io.EOF {
				break
			}
			return err
		}
		if jm.ID != "" {
			id := "pushing layer " + jm.ID
			st, ok := started[id]
			if !ok {
				if jm.Progress != nil || jm.Status == "Pushed" {
					now := time.Now()
					st = &client.VertexStatus{
						ID:      id,
						Started: &now,
					}
					started[id] = st
				} else {
					continue
				}
			}
			st.Timestamp = time.Now()
			if jm.Progress != nil {
				st.Current = jm.Progress.Current
				st.Total = jm.Progress.Total
			}
			if jm.Error != nil {
				now := time.Now()
				st.Completed = &now
			}
			if jm.Status == "Pushed" {
				now := time.Now()
				st.Completed = &now
				st.Current = st.Total
			}
			l.SetStatus(st)
		}
		if jm.Error != nil {
			parsedError = jm.Error
		}
	}
	return nil
}

func remoteDigestWithMoby(ctx context.Context, d *driver.DriverHandle, name string) (string, error) {
	api := d.Config().DockerAPI
	if api == nil {
		return "", errors.Errorf("invalid empty Docker API reference") // should never happen
	}
	creds, err := imagetools.RegistryAuthForRef(name, d.Config().Auth)
	if err != nil {
		return "", err
	}
	img, _, err := api.ImageInspectWithRaw(ctx, name)
	if err != nil {
		return "", err
	}
	if len(img.RepoDigests) == 0 {
		return "", nil
	}
	remoteImage, err := api.DistributionInspect(ctx, name, creds)
	if err != nil {
		return "", err
	}
	return remoteImage.Descriptor.Digest.String(), nil
}

func resultKey(index int, name string) string {
	return fmt.Sprintf("%d-%s", index, name)
}

// detectSharedMounts looks for same local mounts used by multiple requests to the same node
// and creates a separate session that will be used by all detected requests.
func detectSharedMounts(ctx context.Context, reqs map[string][]*reqForNode) (_ map[string][]*session.Session, err error) {
	type fsTracker struct {
		fs fsutil.FS
		so []*client.SolveOpt
	}
	type fsKey struct {
		name string
		dir  string
	}

	m := map[string]map[fsKey]*fsTracker{}
	for _, reqs := range reqs {
		for _, req := range reqs {
			nodeName := req.resolvedNode.Node().Name
			if _, ok := m[nodeName]; !ok {
				m[nodeName] = map[fsKey]*fsTracker{}
			}
			fsMap := m[nodeName]
			for name, m := range req.so.LocalMounts {
				fs, ok := m.(*fs)
				if !ok {
					continue
				}
				key := fsKey{name: name, dir: fs.dir}
				if _, ok := fsMap[key]; !ok {
					fsMap[key] = &fsTracker{fs: fs.FS}
				}
				fsMap[key].so = append(fsMap[key].so, req.so)
			}
		}
	}

	type sharedSession struct {
		*session.Session
		fsMap map[string]fsutil.FS
	}

	sessionMap := map[string][]*sharedSession{}

	defer func() {
		if err != nil {
			for _, sessions := range sessionMap {
				for _, s := range sessions {
					s.Close()
				}
			}
		}
	}()

	for node, fsMap := range m {
		for key, fs := range fsMap {
			if len(fs.so) <= 1 {
				continue
			}

			sessions := sessionMap[node]

			// find session that doesn't have the fs name reserved
			idx := slices.IndexFunc(sessions, func(s *sharedSession) bool {
				_, ok := s.fsMap[key.name]
				return !ok
			})

			var ss *sharedSession
			if idx == -1 {
				s, err := session.NewSession(ctx, fs.so[0].SharedKey)
				if err != nil {
					return nil, err
				}
				ss = &sharedSession{Session: s, fsMap: map[string]fsutil.FS{}}
				sessions = append(sessions, ss)
				sessionMap[node] = sessions
			} else {
				ss = sessions[idx]
			}

			ss.fsMap[key.name] = fs.fs
			for _, so := range fs.so {
				if so.FrontendAttrs == nil {
					so.FrontendAttrs = map[string]string{}
				}
				so.FrontendAttrs["local-sessionid:"+key.name] = ss.ID()
			}
		}
	}

	resetUIDAndGID := func(p string, st *fstypes.Stat) fsutil.MapResult {
		st.Uid = 0
		st.Gid = 0
		return fsutil.MapResultKeep
	}

	// convert back to regular sessions
	sessions := map[string][]*session.Session{}
	for n, ss := range sessionMap {
		arr := make([]*session.Session, 0, len(ss))
		for _, s := range ss {
			arr = append(arr, s.Session)

			src := make(filesync.StaticDirSource, len(s.fsMap))
			for name, fs := range s.fsMap {
				fs, err := fsutil.NewFilterFS(fs, &fsutil.FilterOpt{
					Map: resetUIDAndGID,
				})
				if err != nil {
					return nil, err
				}
				src[name] = fs
			}
			s.Allow(filesync.NewFSSyncProvider(src))
		}
		sessions[n] = arr
	}
	return sessions, nil
}

// calculateChildTargets returns all the targets that depend on current target for reverse index
func calculateChildTargets(reqs map[string][]*reqForNode, opt map[string]Options) map[string][]string {
	out := make(map[string][]string)
	for name := range opt {
		dps := reqs[name]
		for i, dp := range dps {
			so := reqs[name][i].so
			for k, v := range so.FrontendAttrs {
				if strings.HasPrefix(k, "context:") && strings.HasPrefix(v, "target:") {
					target := resultKey(dp.driverIndex, strings.TrimPrefix(v, "target:"))
					out[target] = append(out[target], resultKey(dp.driverIndex, name))
				}
			}
		}
	}
	return out
}

func waitContextDeps(ctx context.Context, index int, results *waitmap.Map, so *client.SolveOpt) error {
	m := map[string][]string{}
	for k, v := range so.FrontendAttrs {
		if strings.HasPrefix(k, "context:") && strings.HasPrefix(v, "target:") {
			target := resultKey(index, strings.TrimPrefix(v, "target:"))
			m[target] = append(m[target], k)
		}
	}
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	res, err := results.Get(ctx, keys...)
	if err != nil {
		return err
	}

	for k, contexts := range m {
		r, ok := res[k]
		if !ok {
			continue
		}
		rr, ok := r.(*gateway.Result)
		if !ok {
			return errors.Errorf("invalid result type %T", rr)
		}
		if so.FrontendAttrs == nil {
			so.FrontendAttrs = map[string]string{}
		}
		if so.FrontendInputs == nil {
			so.FrontendInputs = map[string]llb.State{}
		}

		for _, v := range contexts {
			if len(rr.Refs) > 0 {
				for platform, r := range rr.Refs {
					st, err := r.ToState()
					if err != nil {
						return err
					}
					so.FrontendInputs[k+"::"+platform] = st
					so.FrontendAttrs[v+"::"+platform] = "input:" + k + "::" + platform
					metadata := make(map[string][]byte)
					if dt, ok := rr.Metadata[exptypes.ExporterImageConfigKey+"/"+platform]; ok {
						metadata[exptypes.ExporterImageConfigKey] = dt
					}
					if dt, ok := rr.Metadata["containerimage.buildinfo/"+platform]; ok {
						metadata["containerimage.buildinfo"] = dt
					}
					if len(metadata) > 0 {
						dt, err := json.Marshal(metadata)
						if err != nil {
							return err
						}
						so.FrontendAttrs["input-metadata:"+k+"::"+platform] = string(dt)
					}
				}
				delete(so.FrontendAttrs, v)
			}
			if rr.Ref != nil {
				st, err := rr.Ref.ToState()
				if err != nil {
					return err
				}
				so.FrontendInputs[k] = st
				so.FrontendAttrs[v] = "input:" + k
				metadata := make(map[string][]byte)
				if dt, ok := rr.Metadata[exptypes.ExporterImageConfigKey]; ok {
					metadata[exptypes.ExporterImageConfigKey] = dt
				}
				if dt, ok := rr.Metadata["containerimage.buildinfo"]; ok {
					metadata["containerimage.buildinfo"] = dt
				}
				if len(metadata) > 0 {
					dt, err := json.Marshal(metadata)
					if err != nil {
						return err
					}
					so.FrontendAttrs["input-metadata:"+k] = string(dt)
				}
			}
		}
	}
	return nil
}

func fallbackPrintError(err error, req gateway.SolveRequest) (gateway.SolveRequest, bool) {
	if _, ok := req.FrontendOpt["requestid"]; !ok {
		return req, false
	}

	fallback := false
	fallbackLint := false
	var reqErr *errdefs.UnsupportedSubrequestError
	if errors.As(err, &reqErr) {
		switch reqErr.Name {
		case "frontend.lint":
			fallbackLint = true
			fallthrough
		case "frontend.outline", "frontend.targets":
			fallback = true
		default:
			return req, false
		}
	}

	// buildkit v0.8 vendored in Docker 20.10 does not support typed errors
	for _, req := range []string{"frontend.outline", "frontend.targets", "frontend.lint"} {
		if strings.Contains(err.Error(), "unsupported request "+req) {
			fallback = true
		}
		if req == "frontend.lint" {
			fallbackLint = true
		}
	}

	if fallback {
		req.FrontendOpt["build-arg:BUILDKIT_SYNTAX"] = printFallbackImage
		if fallbackLint {
			req.FrontendOpt["build-arg:BUILDKIT_SYNTAX"] = printLintFallbackImage
		}
		return req, true
	}
	return req, false
}

func noCallFunc(opt map[string]Options) bool {
	for _, v := range opt {
		if v.CallFunc != nil {
			return false
		}
	}
	return true
}

// ReadSourcePolicy reads a source policy from a file.
// The file path is taken from EXPERIMENTAL_BUILDKIT_SOURCE_POLICY env var.
// if the env var is not set, this `returns nil, nil`
func ReadSourcePolicy() (*spb.Policy, error) {
	p := os.Getenv("EXPERIMENTAL_BUILDKIT_SOURCE_POLICY")
	if p == "" {
		return nil, nil
	}

	data, err := os.ReadFile(p)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read policy file")
	}
	var pol spb.Policy
	if err := json.Unmarshal(data, &pol); err != nil {
		// maybe it's in protobuf format?
		e2 := proto.Unmarshal(data, &pol)
		if e2 != nil {
			return nil, errors.Wrap(err, "failed to parse source policy")
		}
	}

	return &pol, nil
}
