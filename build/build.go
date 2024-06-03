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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/containerd/containerd/images"
	"github.com/distribution/reference"
	"github.com/docker/buildx/builder"
	"github.com/docker/buildx/driver"
	"github.com/docker/buildx/util/desktop"
	"github.com/docker/buildx/util/dockerutil"
	"github.com/docker/buildx/util/imagetools"
	"github.com/docker/buildx/util/progress"
	"github.com/docker/buildx/util/resolver"
	"github.com/docker/buildx/util/waitmap"
	"github.com/docker/cli/opts"
	imagetypes "github.com/docker/docker/api/types/image"
	"github.com/docker/docker/pkg/jsonmessage"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/exporter/containerimage/exptypes"
	gateway "github.com/moby/buildkit/frontend/gateway/client"
	"github.com/moby/buildkit/session"
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
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"
)

var (
	errStdinConflict      = errors.New("invalid argument: can't use stdin for both build context and dockerfile")
	errDockerfileConflict = errors.New("ambiguous Dockerfile source: both stdin and flag correspond to Dockerfiles")
)

const (
	printFallbackImage = "docker/dockerfile:1.5@sha256:dbbd5e059e8a07ff7ea6233b213b36aa516b4c53c645f1817a4dd18b83cbea56"
	// moby/buildkit#3d789eb740a93ac814b078fd752307e2a8da5b84
	printLintFallbackImage = "docker.io/docker/dockerfile-upstream:master@sha256:ea5a8efbaf785bdfe6c7ad74f0b11849df42d62861b218429bea17bb078df6ca"
)

type Options struct {
	Inputs Inputs

	Ref           string
	Allow         []entitlements.Entitlement
	Attests       map[string]*string
	BuildArgs     map[string]string
	CacheFrom     []client.CacheOptionsEntry
	CacheTo       []client.CacheOptionsEntry
	CgroupParent  string
	Exports       []client.ExportEntry
	ExtraHosts    []string
	Labels        map[string]string
	NetworkMode   string
	NoCache       bool
	NoCacheFilter []string
	Platforms     []specs.Platform
	Pull          bool
	ShmSize       opts.MemBytes
	Tags          []string
	Target        string
	Ulimits       *opts.UlimitOpt

	Session                []session.Attachable
	Linked                 bool // Linked marks this target as exclusively linked (not requested by the user).
	PrintFunc              *PrintFunc
	WithProvenanceResponse bool
	SourcePolicy           *spb.Policy
	GroupRef               string
}

type PrintFunc struct {
	Name         string
	Format       string
	IgnoreStatus bool
}

type Inputs struct {
	ContextPath      string
	DockerfilePath   string
	InStream         io.Reader
	ContextState     *llb.State
	DockerfileInline string
	NamedContexts    map[string]NamedContext
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

func Build(ctx context.Context, nodes []builder.Node, opt map[string]Options, docker *dockerutil.Client, configDir string, w progress.Writer) (resp map[string]*client.SolveResponse, err error) {
	return BuildWithResultHandler(ctx, nodes, opt, docker, configDir, w, nil)
}

func BuildWithResultHandler(ctx context.Context, nodes []builder.Node, opt map[string]Options, docker *dockerutil.Client, configDir string, w progress.Writer, resultHandleFunc func(driverIndex int, rCtx *ResultHandle)) (resp map[string]*client.SolveResponse, err error) {
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

	if noMobyDriver != nil && !noDefaultLoad() && noPrintFunc(opt) {
		var noOutputTargets []string
		for name, opt := range opt {
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

	drivers, err := resolveDrivers(ctx, nodes, opt, w)
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

	for k, opt := range opt {
		multiDriver := len(drivers[k]) > 1
		hasMobyDriver := false
		gitattrs, addVCSLocalDir, err := getGitAttributes(ctx, opt.Inputs.ContextPath, opt.Inputs.DockerfilePath)
		if err != nil {
			logrus.WithError(err).Warn("current commit information was not captured by the build")
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
			so, release, err := toSolveOpt(ctx, np.Node(), multiDriver, opt, gatewayOpts, configDir, addVCSLocalDir, w, docker)
			if err != nil {
				return nil, err
			}
			if err := saveLocalState(so, k, opt, np.Node(), configDir); err != nil {
				return nil, err
			}
			for k, v := range gitattrs {
				so.FrontendAttrs[k] = v
			}
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
	for name := range opt {
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

	resp = map[string]*client.SolveResponse{}
	var respMu sync.Mutex
	results := waitmap.New()

	multiTarget := len(opt) > 1
	childTargets := calculateChildTargets(reqForNodes, opt)

	for k, opt := range opt {
		err := func(k string) error {
			opt := opt
			dps := drivers[k]
			multiDriver := len(drivers[k]) > 1

			var span trace.Span
			ctx := ctx
			if multiTarget {
				span, ctx = tracing.StartSpan(ctx, k)
			}
			baseCtx := ctx

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
				eg2.Go(func() error {
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
					var printRes map[string][]byte
					buildFunc := func(ctx context.Context, c gateway.Client) (*gateway.Result, error) {
						if opt.PrintFunc != nil {
							if _, ok := req.FrontendOpt["frontend.caps"]; !ok {
								req.FrontendOpt["frontend.caps"] = "moby.buildkit.frontend.subrequests+forward"
							} else {
								req.FrontendOpt["frontend.caps"] += ",moby.buildkit.frontend.subrequests+forward"
							}
							req.FrontendOpt["requestid"] = "frontend." + opt.PrintFunc.Name
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
						if opt.PrintFunc != nil {
							printRes = res.Metadata
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
						rr, err = c.Build(ctx, *so, "buildx", buildFunc, ch)
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
					for k, v := range printRes {
						rr.ExporterResponse[k] = string(v)
					}
					rr.ExporterResponse["buildx.build.ref"] = buildRef
					if opt.WithProvenanceResponse && node.Driver.HistoryAPISupported(ctx) {
						if err := setRecordProvenance(ctx, c, rr, so.Ref, pw); err != nil {
							return err
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

							dt, desc, err := itpull.Combine(ctx, srcs, nil, false)
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

func pushWithMoby(ctx context.Context, d *driver.DriverHandle, name string, l progress.SubLogger) error {
	api := d.Config().DockerAPI
	if api == nil {
		return errors.Errorf("invalid empty Docker API reference") // should never happen
	}
	creds, err := imagetools.RegistryAuthForRef(name, d.Config().Auth)
	if err != nil {
		return err
	}

	rc, err := api.ImagePush(ctx, name, imagetypes.PushOptions{
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
	image, _, err := api.ImageInspectWithRaw(ctx, name)
	if err != nil {
		return "", err
	}
	if len(image.RepoDigests) == 0 {
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

func noPrintFunc(opt map[string]Options) bool {
	for _, v := range opt {
		if v.PrintFunc != nil {
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
		e2 := pol.Unmarshal(data)
		if e2 != nil {
			return nil, errors.Wrap(err, "failed to parse source policy")
		}
	}

	return &pol, nil
}
