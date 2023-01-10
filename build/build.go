package build

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	_ "crypto/sha256" // ensure digests can be computed
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/containerd/containerd/content"
	"github.com/containerd/containerd/content/local"
	"github.com/containerd/containerd/images"
	"github.com/containerd/containerd/platforms"
	"github.com/docker/buildx/builder"
	"github.com/docker/buildx/driver"
	"github.com/docker/buildx/util/dockerutil"
	"github.com/docker/buildx/util/imagetools"
	"github.com/docker/buildx/util/progress"
	"github.com/docker/buildx/util/resolver"
	"github.com/docker/buildx/util/waitmap"
	"github.com/docker/cli/opts"
	"github.com/docker/distribution/reference"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/builder/remotecontext/urlutil"
	"github.com/docker/docker/pkg/jsonmessage"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/client/ociindex"
	"github.com/moby/buildkit/exporter/containerimage/exptypes"
	gateway "github.com/moby/buildkit/frontend/gateway/client"
	"github.com/moby/buildkit/identity"
	"github.com/moby/buildkit/session"
	"github.com/moby/buildkit/session/upload/uploadprovider"
	"github.com/moby/buildkit/solver/errdefs"
	"github.com/moby/buildkit/solver/pb"
	"github.com/moby/buildkit/util/apicaps"
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
	printFallbackImage = "docker/dockerfile-upstream:1.4-outline@sha256:627443ff4e2d0f635d429cfc1da5388bcd5a70949c38adcd3cd7c4e5df67c73c"
)

type Options struct {
	Inputs Inputs

	Allow         []entitlements.Entitlement
	Attests       map[string]*string
	BuildArgs     map[string]string
	CacheFrom     []client.CacheOptionsEntry
	CacheTo       []client.CacheOptionsEntry
	CgroupParent  string
	Exports       []client.ExportEntry
	ExtraHosts    []string
	ImageIDFile   string
	Labels        map[string]string
	NetworkMode   string
	NoCache       bool
	NoCacheFilter []string
	Platforms     []specs.Platform
	Pull          bool
	Session       []session.Attachable
	ShmSize       opts.MemBytes
	Tags          []string
	Target        string
	Ulimits       *opts.UlimitOpt

	// Linked marks this target as exclusively linked (not requested by the user).
	Linked    bool
	PrintFunc *PrintFunc
}

type PrintFunc struct {
	Name   string
	Format string
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

type driverPair struct {
	driverIndex int
	platforms   []specs.Platform
	so          *client.SolveOpt
	bopts       gateway.BuildOpts
}

func driverIndexes(m map[string][]driverPair) []int {
	out := make([]int, 0, len(m))
	visited := map[int]struct{}{}
	for _, dp := range m {
		for _, d := range dp {
			if _, ok := visited[d.driverIndex]; ok {
				continue
			}
			visited[d.driverIndex] = struct{}{}
			out = append(out, d.driverIndex)
		}
	}
	return out
}

func allIndexes(l int) []int {
	out := make([]int, 0, l)
	for i := 0; i < l; i++ {
		out = append(out, i)
	}
	return out
}

func ensureBooted(ctx context.Context, nodes []builder.Node, idxs []int, pw progress.Writer) ([]*client.Client, error) {
	clients := make([]*client.Client, len(nodes))

	baseCtx := ctx
	eg, ctx := errgroup.WithContext(ctx)

	for _, i := range idxs {
		func(i int) {
			eg.Go(func() error {
				c, err := driver.Boot(ctx, baseCtx, nodes[i].Driver, pw)
				if err != nil {
					return err
				}
				clients[i] = c
				return nil
			})
		}(i)
	}

	if err := eg.Wait(); err != nil {
		return nil, err
	}

	return clients, nil
}

func splitToDriverPairs(availablePlatforms map[string]int, opt map[string]Options) map[string][]driverPair {
	m := map[string][]driverPair{}
	for k, opt := range opt {
		mm := map[int][]specs.Platform{}
		for _, p := range opt.Platforms {
			k := platforms.Format(p)
			idx := availablePlatforms[k] // default 0
			pp := mm[idx]
			pp = append(pp, p)
			mm[idx] = pp
		}
		// if no platform is specified, use first driver
		if len(mm) == 0 {
			mm[0] = nil
		}
		dps := make([]driverPair, 0, 2)
		for idx, pp := range mm {
			dps = append(dps, driverPair{driverIndex: idx, platforms: pp})
		}
		m[k] = dps
	}
	return m
}

func resolveDrivers(ctx context.Context, nodes []builder.Node, opt map[string]Options, pw progress.Writer) (map[string][]driverPair, []*client.Client, error) {
	dps, clients, err := resolveDriversBase(ctx, nodes, opt, pw)
	if err != nil {
		return nil, nil, err
	}

	bopts := make([]gateway.BuildOpts, len(clients))

	span, ctx := tracing.StartSpan(ctx, "load buildkit capabilities", trace.WithSpanKind(trace.SpanKindInternal))

	eg, ctx := errgroup.WithContext(ctx)
	for i, c := range clients {
		if c == nil {
			continue
		}

		func(i int, c *client.Client) {
			eg.Go(func() error {
				clients[i].Build(ctx, client.SolveOpt{
					Internal: true,
				}, "buildx", func(ctx context.Context, c gateway.Client) (*gateway.Result, error) {
					bopts[i] = c.BuildOpts()
					return nil, nil
				}, nil)
				return nil
			})
		}(i, c)
	}

	err = eg.Wait()
	tracing.FinishWithError(span, err)
	if err != nil {
		return nil, nil, err
	}
	for key := range dps {
		for i, dp := range dps[key] {
			dps[key][i].bopts = bopts[dp.driverIndex]
		}
	}

	return dps, clients, nil
}

func resolveDriversBase(ctx context.Context, nodes []builder.Node, opt map[string]Options, pw progress.Writer) (map[string][]driverPair, []*client.Client, error) {
	availablePlatforms := map[string]int{}
	for i, node := range nodes {
		for _, p := range node.Platforms {
			availablePlatforms[platforms.Format(p)] = i
		}
	}

	undetectedPlatform := false
	allPlatforms := map[string]int{}
	for _, opt := range opt {
		for _, p := range opt.Platforms {
			k := platforms.Format(p)
			allPlatforms[k] = -1
			if _, ok := availablePlatforms[k]; !ok {
				undetectedPlatform = true
			}
		}
	}

	// fast path
	if len(nodes) == 1 || len(allPlatforms) == 0 {
		m := map[string][]driverPair{}
		for k, opt := range opt {
			m[k] = []driverPair{{driverIndex: 0, platforms: opt.Platforms}}
		}
		clients, err := ensureBooted(ctx, nodes, driverIndexes(m), pw)
		if err != nil {
			return nil, nil, err
		}
		return m, clients, nil
	}

	// map based on existing platforms
	if !undetectedPlatform {
		m := splitToDriverPairs(availablePlatforms, opt)
		clients, err := ensureBooted(ctx, nodes, driverIndexes(m), pw)
		if err != nil {
			return nil, nil, err
		}
		return m, clients, nil
	}

	// boot all drivers in k
	clients, err := ensureBooted(ctx, nodes, allIndexes(len(nodes)), pw)
	if err != nil {
		return nil, nil, err
	}

	eg, ctx := errgroup.WithContext(ctx)
	workers := make([][]*client.WorkerInfo, len(clients))

	for i, c := range clients {
		if c == nil {
			continue
		}
		func(i int) {
			eg.Go(func() error {
				ww, err := clients[i].ListWorkers(ctx)
				if err != nil {
					return errors.Wrap(err, "listing workers")
				}
				workers[i] = ww
				return nil
			})

		}(i)
	}

	if err := eg.Wait(); err != nil {
		return nil, nil, err
	}

	for i, ww := range workers {
		for _, w := range ww {
			for _, p := range w.Platforms {
				p = platforms.Normalize(p)
				ps := platforms.Format(p)

				if _, ok := availablePlatforms[ps]; !ok {
					availablePlatforms[ps] = i
				}
			}
		}
	}

	return splitToDriverPairs(availablePlatforms, opt), clients, nil
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

func toSolveOpt(ctx context.Context, node builder.Node, multiDriver bool, opt Options, bopts gateway.BuildOpts, configDir string, pw progress.Writer, dl dockerLoadCallback) (solveOpt *client.SolveOpt, release func(), err error) {
	nodeDriver := node.Driver
	defers := make([]func(), 0, 2)
	releaseF := func() {
		for _, f := range defers {
			f()
		}
	}

	defer func() {
		if err != nil {
			releaseF()
		}
	}()

	if opt.ImageIDFile != "" {
		// Avoid leaving a stale file if we eventually fail
		if err := os.Remove(opt.ImageIDFile); err != nil && !os.IsNotExist(err) {
			return nil, nil, errors.Wrap(err, "removing image ID file")
		}
	}

	// inline cache from build arg
	if v, ok := opt.BuildArgs["BUILDKIT_INLINE_CACHE"]; ok {
		if v, _ := strconv.ParseBool(v); v {
			opt.CacheTo = append(opt.CacheTo, client.CacheOptionsEntry{
				Type:  "inline",
				Attrs: map[string]string{},
			})
		}
	}

	for _, e := range opt.CacheTo {
		if e.Type != "inline" && !nodeDriver.Features()[driver.CacheExport] {
			return nil, nil, notSupported(nodeDriver, driver.CacheExport)
		}
	}

	cacheTo := make([]client.CacheOptionsEntry, 0, len(opt.CacheTo))
	for _, e := range opt.CacheTo {
		if e.Type == "gha" {
			if !bopts.LLBCaps.Contains(apicaps.CapID("cache.gha")) {
				continue
			}
		} else if e.Type == "s3" {
			if !bopts.LLBCaps.Contains(apicaps.CapID("cache.s3")) {
				continue
			}
		}
		cacheTo = append(cacheTo, e)
	}

	cacheFrom := make([]client.CacheOptionsEntry, 0, len(opt.CacheFrom))
	for _, e := range opt.CacheFrom {
		if e.Type == "gha" {
			if !bopts.LLBCaps.Contains(apicaps.CapID("cache.gha")) {
				continue
			}
		} else if e.Type == "s3" {
			if !bopts.LLBCaps.Contains(apicaps.CapID("cache.s3")) {
				continue
			}
		}
		cacheFrom = append(cacheFrom, e)
	}

	so := client.SolveOpt{
		Frontend:            "dockerfile.v0",
		FrontendAttrs:       map[string]string{},
		LocalDirs:           map[string]string{},
		CacheExports:        cacheTo,
		CacheImports:        cacheFrom,
		AllowedEntitlements: opt.Allow,
	}

	if opt.CgroupParent != "" {
		so.FrontendAttrs["cgroup-parent"] = opt.CgroupParent
	}

	if v, ok := opt.BuildArgs["BUILDKIT_MULTI_PLATFORM"]; ok {
		if v, _ := strconv.ParseBool(v); v {
			so.FrontendAttrs["multi-platform"] = "true"
		}
	}

	if multiDriver {
		// force creation of manifest list
		so.FrontendAttrs["multi-platform"] = "true"
	}

	attests := make(map[string]string)
	for k, v := range opt.Attests {
		if v != nil {
			attests[k] = *v
		}
	}
	supportsAttestations := bopts.LLBCaps.Contains(apicaps.CapID("exporter.image.attestations"))
	if len(attests) > 0 {
		if !supportsAttestations {
			return nil, nil, errors.Errorf("attestations are not supported by the current buildkitd")
		}
		for k, v := range attests {
			so.FrontendAttrs[k] = v
		}
	}
	if _, ok := opt.Attests["attest:provenance"]; !ok && supportsAttestations {
		so.FrontendAttrs["attest:provenance"] = "mode=min,inline-only=true"
	}

	switch len(opt.Exports) {
	case 1:
		// valid
	case 0:
		if nodeDriver.IsMobyDriver() && !noDefaultLoad() {
			// backwards compat for docker driver only:
			// this ensures the build results in a docker image.
			opt.Exports = []client.ExportEntry{{Type: "image", Attrs: map[string]string{}}}
		}
	default:
		return nil, nil, errors.Errorf("multiple outputs currently unsupported")
	}

	// fill in image exporter names from tags
	if len(opt.Tags) > 0 {
		tags := make([]string, len(opt.Tags))
		for i, tag := range opt.Tags {
			ref, err := reference.Parse(tag)
			if err != nil {
				return nil, nil, errors.Wrapf(err, "invalid tag %q", tag)
			}
			tags[i] = ref.String()
		}
		for i, e := range opt.Exports {
			switch e.Type {
			case "image", "oci", "docker":
				opt.Exports[i].Attrs["name"] = strings.Join(tags, ",")
			}
		}
	} else {
		for _, e := range opt.Exports {
			if e.Type == "image" && e.Attrs["name"] == "" && e.Attrs["push"] != "" {
				if ok, _ := strconv.ParseBool(e.Attrs["push"]); ok {
					return nil, nil, errors.Errorf("tag is needed when pushing to registry")
				}
			}
		}
	}

	// cacheonly is a fake exporter to opt out of default behaviors
	exports := make([]client.ExportEntry, 0, len(opt.Exports))
	for _, e := range opt.Exports {
		if e.Type != "cacheonly" {
			exports = append(exports, e)
		}
	}
	opt.Exports = exports

	// set up exporters
	for i, e := range opt.Exports {
		if (e.Type == "local" || e.Type == "tar") && opt.ImageIDFile != "" {
			return nil, nil, errors.Errorf("local and tar exporters are incompatible with image ID file")
		}
		if e.Type == "oci" && !nodeDriver.Features()[driver.OCIExporter] {
			return nil, nil, notSupported(nodeDriver, driver.OCIExporter)
		}
		if e.Type == "docker" {
			if len(opt.Platforms) > 1 || len(attests) > 0 {
				return nil, nil, errors.Errorf("docker exporter does not currently support exporting manifest lists")
			}
			if e.Output == nil {
				if nodeDriver.IsMobyDriver() {
					e.Type = "image"
				} else {
					w, cancel, err := dl(e.Attrs["context"])
					if err != nil {
						return nil, nil, err
					}
					defers = append(defers, cancel)
					opt.Exports[i].Output = wrapWriteCloser(w)
				}
			} else if !nodeDriver.Features()[driver.DockerExporter] {
				return nil, nil, notSupported(nodeDriver, driver.DockerExporter)
			}
		}
		if e.Type == "image" && nodeDriver.IsMobyDriver() {
			opt.Exports[i].Type = "moby"
			if e.Attrs["push"] != "" {
				if ok, _ := strconv.ParseBool(e.Attrs["push"]); ok {
					if ok, _ := strconv.ParseBool(e.Attrs["push-by-digest"]); ok {
						return nil, nil, errors.Errorf("push-by-digest is currently not implemented for docker driver, please create a new builder instance")
					}
				}
			}
		}
		if e.Type == "docker" || e.Type == "image" || e.Type == "oci" {
			// inline buildinfo attrs from build arg
			if v, ok := opt.BuildArgs["BUILDKIT_INLINE_BUILDINFO_ATTRS"]; ok {
				e.Attrs["buildinfo-attrs"] = v
			}
		}
	}

	so.Exports = opt.Exports
	so.Session = opt.Session

	releaseLoad, err := LoadInputs(ctx, nodeDriver, opt.Inputs, pw, &so)
	if err != nil {
		return nil, nil, err
	}
	defers = append(defers, releaseLoad)

	if sharedKey := so.LocalDirs["context"]; sharedKey != "" {
		if p, err := filepath.Abs(sharedKey); err == nil {
			sharedKey = filepath.Base(p)
		}
		so.SharedKey = sharedKey + ":" + tryNodeIdentifier(configDir)
	}

	if opt.Pull {
		so.FrontendAttrs["image-resolve-mode"] = "pull"
	}
	if opt.Target != "" {
		so.FrontendAttrs["target"] = opt.Target
	}
	if len(opt.NoCacheFilter) > 0 {
		so.FrontendAttrs["no-cache"] = strings.Join(opt.NoCacheFilter, ",")
	}
	if opt.NoCache {
		so.FrontendAttrs["no-cache"] = ""
	}
	for k, v := range opt.BuildArgs {
		so.FrontendAttrs["build-arg:"+k] = v
	}
	for k, v := range opt.Labels {
		so.FrontendAttrs["label:"+k] = v
	}

	for k, v := range node.ProxyConfig {
		if _, ok := opt.BuildArgs[k]; !ok {
			so.FrontendAttrs["build-arg:"+k] = v
		}
	}

	// Propagate SOURCE_DATE_EPOCH from the client env
	if v := os.Getenv("SOURCE_DATE_EPOCH"); v != "" {
		if _, ok := so.FrontendAttrs["build-arg:SOURCE_DATE_EPOCH"]; !ok {
			so.FrontendAttrs["build-arg:SOURCE_DATE_EPOCH"] = v
		}
	}

	// set platforms
	if len(opt.Platforms) != 0 {
		pp := make([]string, len(opt.Platforms))
		for i, p := range opt.Platforms {
			pp[i] = platforms.Format(p)
		}
		if len(pp) > 1 && !nodeDriver.Features()[driver.MultiPlatform] {
			return nil, nil, notSupported(nodeDriver, driver.MultiPlatform)
		}
		so.FrontendAttrs["platform"] = strings.Join(pp, ",")
	}

	// setup networkmode
	switch opt.NetworkMode {
	case "host":
		so.FrontendAttrs["force-network-mode"] = opt.NetworkMode
		so.AllowedEntitlements = append(so.AllowedEntitlements, entitlements.EntitlementNetworkHost)
	case "none":
		so.FrontendAttrs["force-network-mode"] = opt.NetworkMode
	case "", "default":
	default:
		return nil, nil, errors.Errorf("network mode %q not supported by buildkit - you can define a custom network for your builder using the network driver-opt in buildx create", opt.NetworkMode)
	}

	// setup extrahosts
	extraHosts, err := toBuildkitExtraHosts(opt.ExtraHosts, nodeDriver.IsMobyDriver())
	if err != nil {
		return nil, nil, err
	}
	if len(extraHosts) > 0 {
		so.FrontendAttrs["add-hosts"] = extraHosts
	}

	// setup shm size
	if opt.ShmSize.Value() > 0 {
		so.FrontendAttrs["shm-size"] = strconv.FormatInt(opt.ShmSize.Value(), 10)
	}

	// setup ulimits
	ulimits, err := toBuildkitUlimits(opt.Ulimits)
	if err != nil {
		return nil, nil, err
	} else if len(ulimits) > 0 {
		so.FrontendAttrs["ulimit"] = ulimits
	}

	return &so, releaseF, nil
}

// ContainerConfig is configuration for a container to run.
type ContainerConfig struct {
	ResultCtx *ResultContext

	Stdin  io.ReadCloser
	Stdout io.WriteCloser
	Stderr io.WriteCloser
	Tty    bool

	Entrypoint []string
	Cmd        []string
	Env        []string
	User       *string
	Cwd        *string
}

// ResultContext is a build result with the client that built it.
type ResultContext struct {
	Client *client.Client
	Res    *gateway.Result
}

// Invoke invokes a build result as a container.
func Invoke(ctx context.Context, cfg ContainerConfig) error {
	if cfg.ResultCtx == nil {
		return errors.Errorf("result must be provided")
	}
	c, res := cfg.ResultCtx.Client, cfg.ResultCtx.Res

	mainCtx := ctx

	_, err := c.Build(context.TODO(), client.SolveOpt{}, "buildx", func(ctx context.Context, c gateway.Client) (*gateway.Result, error) {
		ctx, cancel := context.WithCancel(ctx)
		go func() {
			<-mainCtx.Done()
			cancel()
		}()

		if res.Ref == nil {
			return nil, errors.Errorf("no reference is registered")
		}
		st, err := res.Ref.ToState()
		if err != nil {
			return nil, err
		}
		def, err := st.Marshal(ctx)
		if err != nil {
			return nil, err
		}
		imgRef, err := c.Solve(ctx, gateway.SolveRequest{
			Definition: def.ToPB(),
		})
		if err != nil {
			return nil, err
		}
		ctr, err := c.NewContainer(ctx, gateway.NewContainerRequest{
			Mounts: []gateway.Mount{
				{
					Dest:      "/",
					MountType: pb.MountType_BIND,
					Ref:       imgRef.Ref,
				},
			},
		})
		if err != nil {
			return nil, err
		}
		defer ctr.Release(context.TODO())

		imgData := res.Metadata[exptypes.ExporterImageConfigKey]
		var img *specs.Image
		if len(imgData) > 0 {
			img = &specs.Image{}
			if err := json.Unmarshal(imgData, img); err != nil {
				fmt.Println(err)
				return nil, err
			}
		}

		user := ""
		if cfg.User != nil {
			user = *cfg.User
		} else if img != nil {
			user = img.Config.User
		}

		cwd := ""
		if cfg.Cwd != nil {
			cwd = *cfg.Cwd
		} else if img != nil {
			cwd = img.Config.WorkingDir
		}

		env := []string{}
		if img != nil {
			env = append(env, img.Config.Env...)
		}
		env = append(env, cfg.Env...)

		args := []string{}
		if cfg.Entrypoint != nil {
			args = append(args, cfg.Entrypoint...)
		} else if img != nil {
			args = append(args, img.Config.Entrypoint...)
		}
		if cfg.Cmd != nil {
			args = append(args, cfg.Cmd...)
		} else if img != nil {
			args = append(args, img.Config.Cmd...)
		}

		proc, err := ctr.Start(ctx, gateway.StartRequest{
			Args:   args,
			Env:    env,
			User:   user,
			Cwd:    cwd,
			Tty:    cfg.Tty,
			Stdin:  cfg.Stdin,
			Stdout: cfg.Stdout,
			Stderr: cfg.Stderr,
		})
		if err != nil {
			return nil, errors.Errorf("failed to start container: %v", err)
		}
		errCh := make(chan error)
		doneCh := make(chan struct{})
		go func() {
			if err := proc.Wait(); err != nil {
				errCh <- err
				return
			}
			close(doneCh)
		}()
		select {
		case <-doneCh:
		case <-ctx.Done():
			return nil, ctx.Err()
		case err := <-errCh:
			return nil, err
		}
		return nil, nil
	}, nil)
	return err
}

func Build(ctx context.Context, nodes []builder.Node, opt map[string]Options, docker *dockerutil.Client, configDir string, w progress.Writer) (resp map[string]*client.SolveResponse, err error) {
	return BuildWithResultHandler(ctx, nodes, opt, docker, configDir, w, nil, false)
}

func BuildWithResultHandler(ctx context.Context, nodes []builder.Node, opt map[string]Options, docker *dockerutil.Client, configDir string, w progress.Writer, resultHandleFunc func(driverIndex int, rCtx *ResultContext), allowNoOutput bool) (resp map[string]*client.SolveResponse, err error) {
	if len(nodes) == 0 {
		return nil, errors.Errorf("driver required for build")
	}

	nodes, err = filterAvailableNodes(nodes)
	if err != nil {
		return nil, errors.Wrapf(err, "no valid drivers found")
	}

	var noMobyDriver driver.Driver
	for _, n := range nodes {
		if !n.Driver.IsMobyDriver() {
			noMobyDriver = n.Driver
			break
		}
	}

	if noMobyDriver != nil && !noDefaultLoad() && noPrintFunc(opt) {
		var noOutputTargets []string
		for name, opt := range opt {
			if !opt.Linked && len(opt.Exports) == 0 {
				noOutputTargets = append(noOutputTargets, name)
			}
		}
		if len(noOutputTargets) > 0 && !allowNoOutput {
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

	m, clients, err := resolveDrivers(ctx, nodes, opt, w)
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

	eg, ctx := errgroup.WithContext(ctx)

	for k, opt := range opt {
		multiDriver := len(m[k]) > 1
		hasMobyDriver := false
		gitattrs, err := getGitAttributes(ctx, opt.Inputs.ContextPath, opt.Inputs.DockerfilePath)
		if err != nil {
			logrus.Warn(err)
		}
		for i, np := range m[k] {
			node := nodes[np.driverIndex]
			if node.Driver.IsMobyDriver() {
				hasMobyDriver = true
			}
			opt.Platforms = np.platforms
			so, release, err := toSolveOpt(ctx, node, multiDriver, opt, np.bopts, configDir, w, func(name string) (io.WriteCloser, func(), error) {
				return docker.LoadImage(ctx, name, w)
			})
			if err != nil {
				return nil, err
			}
			for k, v := range gitattrs {
				so.FrontendAttrs[k] = v
			}
			defers = append(defers, release)
			m[k][i].so = so
		}
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
			for _, dp := range m[k] {
				for _, e := range dp.so.Exports {
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
		dps := m[name]
		for _, dp := range dps {
			for k, v := range dp.so.FrontendAttrs {
				if strings.HasPrefix(k, "context:") && strings.HasPrefix(v, "target:") {
					k2 := strings.TrimPrefix(v, "target:")
					dps2, ok := m[k2]
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

	for k, opt := range opt {
		err := func(k string) error {
			opt := opt
			dps := m[k]
			multiDriver := len(m[k]) > 1

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
				i, dp, so := i, dp, *dp.so
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

				c := clients[dp.driverIndex]
				eg2.Go(func() error {
					pw = progress.ResetTime(pw)

					if err := waitContextDeps(ctx, dp.driverIndex, results, &so); err != nil {
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
					rr, err := c.Build(ctx, so, "buildx", func(ctx context.Context, c gateway.Client) (*gateway.Result, error) {
						var isFallback bool
						var origErr error
						for {
							if opt.PrintFunc != nil {
								if _, ok := req.FrontendOpt["frontend.caps"]; !ok {
									req.FrontendOpt["frontend.caps"] = "moby.buildkit.frontend.subrequests+forward"
								} else {
									req.FrontendOpt["frontend.caps"] += ",moby.buildkit.frontend.subrequests+forward"
								}
								req.FrontendOpt["requestid"] = "frontend." + opt.PrintFunc.Name
								if isFallback {
									req.FrontendOpt["build-arg:BUILDKIT_SYNTAX"] = printFallbackImage
								}
							}
							res, err := c.Solve(ctx, req)
							if err != nil {
								if origErr != nil {
									return nil, err
								}
								var reqErr *errdefs.UnsupportedSubrequestError
								if !isFallback {
									if errors.As(err, &reqErr) {
										switch reqErr.Name {
										case "frontend.outline", "frontend.targets":
											isFallback = true
											origErr = err
											continue
										}
										return nil, err
									}
									// buildkit v0.8 vendored in Docker 20.10 does not support typed errors
									if strings.Contains(err.Error(), "unsupported request frontend.outline") || strings.Contains(err.Error(), "unsupported request frontend.targets") {
										isFallback = true
										origErr = err
										continue
									}
								}
								return nil, err
							}
							if opt.PrintFunc != nil {
								printRes = res.Metadata
							}
							results.Set(resultKey(dp.driverIndex, k), res)
							if resultHandleFunc != nil {
								resultHandleFunc(dp.driverIndex, &ResultContext{cc, res})
							}
							return res, nil
						}
					}, ch)
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

					node := nodes[dp.driverIndex].Driver
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
					dgst := res[0].ExporterResponse[exptypes.ExporterImageDigestKey]
					if v, ok := res[0].ExporterResponse[exptypes.ExporterImageConfigDigestKey]; ok {
						dgst = v
					}
					if opt.ImageIDFile != "" {
						return os.WriteFile(opt.ImageIDFile, []byte(dgst), 0644)
					}
					return nil
				}

				if pushNames != "" {
					progress.Write(pw, fmt.Sprintf("merging manifest list %s", pushNames), func() error {
						descs := make([]specs.Descriptor, 0, len(res))

						for _, r := range res {
							s, ok := r.ExporterResponse[exptypes.ExporterImageDigestKey]
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
								imageopt = nodes[dp.driverIndex].ImageOpt
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

							dt, desc, err := itpull.Combine(ctx, srcs)
							if err != nil {
								return err
							}
							if opt.ImageIDFile != "" {
								if err := os.WriteFile(opt.ImageIDFile, []byte(desc.Digest), 0644); err != nil {
									return err
								}
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
									"containerimage.digest": desc.Digest.String(),
								},
							}
							respMu.Unlock()
						}
						return nil
					})
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

func pushWithMoby(ctx context.Context, d driver.Driver, name string, l progress.SubLogger) error {
	api := d.Config().DockerAPI
	if api == nil {
		return errors.Errorf("invalid empty Docker API reference") // should never happen
	}
	creds, err := imagetools.RegistryAuthForRef(name, d.Config().Auth)
	if err != nil {
		return err
	}

	rc, err := api.ImagePush(ctx, name, types.ImagePushOptions{
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

func remoteDigestWithMoby(ctx context.Context, d driver.Driver, name string) (string, error) {
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

func createTempDockerfile(r io.Reader) (string, error) {
	dir, err := os.MkdirTemp("", "dockerfile")
	if err != nil {
		return "", err
	}
	f, err := os.Create(filepath.Join(dir, "Dockerfile"))
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.Copy(f, r); err != nil {
		return "", err
	}
	return dir, err
}

func LoadInputs(ctx context.Context, d driver.Driver, inp Inputs, pw progress.Writer, target *client.SolveOpt) (func(), error) {
	if inp.ContextPath == "" {
		return nil, errors.New("please specify build context (e.g. \".\" for the current directory)")
	}

	// TODO: handle stdin, symlinks, remote contexts, check files exist

	var (
		err              error
		dockerfileReader io.Reader
		dockerfileDir    string
		dockerfileName   = inp.DockerfilePath
		toRemove         []string
	)

	switch {
	case inp.ContextState != nil:
		if target.FrontendInputs == nil {
			target.FrontendInputs = make(map[string]llb.State)
		}
		target.FrontendInputs["context"] = *inp.ContextState
		target.FrontendInputs["dockerfile"] = *inp.ContextState
	case inp.ContextPath == "-":
		if inp.DockerfilePath == "-" {
			return nil, errStdinConflict
		}

		buf := bufio.NewReader(inp.InStream)
		magic, err := buf.Peek(archiveHeaderSize * 2)
		if err != nil && err != io.EOF {
			return nil, errors.Wrap(err, "failed to peek context header from STDIN")
		}
		if !(err == io.EOF && len(magic) == 0) {
			if isArchive(magic) {
				// stdin is context
				up := uploadprovider.New()
				target.FrontendAttrs["context"] = up.Add(buf)
				target.Session = append(target.Session, up)
			} else {
				if inp.DockerfilePath != "" {
					return nil, errDockerfileConflict
				}
				// stdin is dockerfile
				dockerfileReader = buf
				inp.ContextPath, _ = os.MkdirTemp("", "empty-dir")
				toRemove = append(toRemove, inp.ContextPath)
				target.LocalDirs["context"] = inp.ContextPath
			}
		}

	case isLocalDir(inp.ContextPath):
		target.LocalDirs["context"] = inp.ContextPath
		switch inp.DockerfilePath {
		case "-":
			dockerfileReader = inp.InStream
		case "":
			dockerfileDir = inp.ContextPath
		default:
			dockerfileDir = filepath.Dir(inp.DockerfilePath)
			dockerfileName = filepath.Base(inp.DockerfilePath)
		}

	case urlutil.IsGitURL(inp.ContextPath), urlutil.IsURL(inp.ContextPath):
		if inp.DockerfilePath == "-" {
			dockerfileReader = inp.InStream
		}
		target.FrontendAttrs["context"] = inp.ContextPath
	default:
		return nil, errors.Errorf("unable to prepare context: path %q not found", inp.ContextPath)
	}

	if inp.DockerfileInline != "" {
		dockerfileReader = strings.NewReader(inp.DockerfileInline)
	}

	if dockerfileReader != nil {
		dockerfileDir, err = createTempDockerfile(dockerfileReader)
		if err != nil {
			return nil, err
		}
		toRemove = append(toRemove, dockerfileDir)
		dockerfileName = "Dockerfile"
		target.FrontendAttrs["dockerfilekey"] = "dockerfile"
	}
	if urlutil.IsURL(inp.DockerfilePath) {
		dockerfileDir, err = createTempDockerfileFromURL(ctx, d, inp.DockerfilePath, pw)
		if err != nil {
			return nil, err
		}
		toRemove = append(toRemove, dockerfileDir)
		dockerfileName = "Dockerfile"
		target.FrontendAttrs["dockerfilekey"] = "dockerfile"
		delete(target.FrontendInputs, "dockerfile")
	}

	if dockerfileName == "" {
		dockerfileName = "Dockerfile"
	}

	if dockerfileDir != "" {
		target.LocalDirs["dockerfile"] = dockerfileDir
		dockerfileName = handleLowercaseDockerfile(dockerfileDir, dockerfileName)
	}

	target.FrontendAttrs["filename"] = dockerfileName

	for k, v := range inp.NamedContexts {
		target.FrontendAttrs["frontend.caps"] = "moby.buildkit.frontend.contexts+forward"
		if v.State != nil {
			target.FrontendAttrs["context:"+k] = "input:" + k
			if target.FrontendInputs == nil {
				target.FrontendInputs = make(map[string]llb.State)
			}
			target.FrontendInputs[k] = *v.State
			continue
		}

		if urlutil.IsGitURL(v.Path) || urlutil.IsURL(v.Path) || strings.HasPrefix(v.Path, "docker-image://") || strings.HasPrefix(v.Path, "target:") {
			target.FrontendAttrs["context:"+k] = v.Path
			continue
		}

		// handle OCI layout
		if strings.HasPrefix(v.Path, "oci-layout://") {
			pathAlone := strings.TrimPrefix(v.Path, "oci-layout://")
			localPath := pathAlone
			localPath, dig, hasDigest := strings.Cut(localPath, "@")
			localPath, tag, hasTag := strings.Cut(localPath, ":")
			if !hasTag {
				tag = "latest"
				hasTag = true
			}
			idx := ociindex.NewStoreIndex(localPath)
			if !hasDigest {
				// lookup by name
				desc, err := idx.Get(tag)
				if err != nil {
					return nil, err
				}
				if desc != nil {
					dig = string(desc.Digest)
					hasDigest = true
				}
			}
			if !hasDigest {
				// lookup single
				desc, err := idx.GetSingle()
				if err != nil {
					return nil, err
				}
				if desc != nil {
					dig = string(desc.Digest)
					hasDigest = true
				}
			}
			if !hasDigest {
				return nil, errors.Errorf("oci-layout reference %q could not be resolved", v.Path)
			}
			_, err := digest.Parse(dig)
			if err != nil {
				return nil, errors.Wrapf(err, "invalid oci-layout digest %s", dig)
			}

			store, err := local.NewStore(localPath)
			if err != nil {
				return nil, errors.Wrapf(err, "invalid store at %s", localPath)
			}
			storeName := identity.NewID()
			if target.OCIStores == nil {
				target.OCIStores = map[string]content.Store{}
			}
			target.OCIStores[storeName] = store

			layout := "oci-layout://" + storeName
			if hasTag {
				layout += ":" + tag
			}
			if hasDigest {
				layout += "@" + dig
			}

			target.FrontendAttrs["context:"+k] = layout
			continue
		}
		st, err := os.Stat(v.Path)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to get build context %v", k)
		}
		if !st.IsDir() {
			return nil, errors.Wrapf(syscall.ENOTDIR, "failed to get build context path %v", v)
		}
		localName := k
		if k == "context" || k == "dockerfile" {
			localName = "_" + k // underscore to avoid collisions
		}
		target.LocalDirs[localName] = v.Path
		target.FrontendAttrs["context:"+k] = "local:" + localName
	}

	release := func() {
		for _, dir := range toRemove {
			os.RemoveAll(dir)
		}
	}
	return release, nil
}

func resultKey(index int, name string) string {
	return fmt.Sprintf("%d-%s", index, name)
}

func waitContextDeps(ctx context.Context, index int, results *waitmap.Map, so *client.SolveOpt) error {
	m := map[string]string{}
	for k, v := range so.FrontendAttrs {
		if strings.HasPrefix(k, "context:") && strings.HasPrefix(v, "target:") {
			target := resultKey(index, strings.TrimPrefix(v, "target:"))
			m[target] = k
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

	for k, v := range m {
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
				if dt, ok := rr.Metadata[exptypes.ExporterBuildInfo+"/"+platform]; ok {
					metadata[exptypes.ExporterBuildInfo] = dt
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
			if dt, ok := rr.Metadata[exptypes.ExporterBuildInfo]; ok {
				metadata[exptypes.ExporterBuildInfo] = dt
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
	return nil
}

func notSupported(d driver.Driver, f driver.Feature) error {
	return errors.Errorf("%s feature is currently not supported for %s driver. Please switch to a different driver (eg. \"docker buildx create --use\")", f, d.Factory().Name())
}

type dockerLoadCallback func(name string) (io.WriteCloser, func(), error)

func noDefaultLoad() bool {
	v, ok := os.LookupEnv("BUILDX_NO_DEFAULT_LOAD")
	if !ok {
		return false
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		logrus.Warnf("invalid non-bool value for BUILDX_NO_DEFAULT_LOAD: %s", v)
	}
	return b
}

// handle https://github.com/moby/moby/pull/10858
func handleLowercaseDockerfile(dir, p string) string {
	if filepath.Base(p) != "Dockerfile" {
		return p
	}

	f, err := os.Open(filepath.Dir(filepath.Join(dir, p)))
	if err != nil {
		return p
	}

	names, err := f.Readdirnames(-1)
	if err != nil {
		return p
	}

	foundLowerCase := false
	for _, n := range names {
		if n == "Dockerfile" {
			return p
		}
		if n == "dockerfile" {
			foundLowerCase = true
		}
	}
	if foundLowerCase {
		return filepath.Join(filepath.Dir(p), "dockerfile")
	}
	return p
}

func wrapWriteCloser(wc io.WriteCloser) func(map[string]string) (io.WriteCloser, error) {
	return func(map[string]string) (io.WriteCloser, error) {
		return wc, nil
	}
}

var nodeIdentifierMu sync.Mutex

func tryNodeIdentifier(configDir string) (out string) {
	nodeIdentifierMu.Lock()
	defer nodeIdentifierMu.Unlock()
	sessionFile := filepath.Join(configDir, ".buildNodeID")
	if _, err := os.Lstat(sessionFile); err != nil {
		if os.IsNotExist(err) { // create a new file with stored randomness
			b := make([]byte, 8)
			if _, err := rand.Read(b); err != nil {
				return out
			}
			if err := os.WriteFile(sessionFile, []byte(hex.EncodeToString(b)), 0600); err != nil {
				return out
			}
		}
	}

	dt, err := os.ReadFile(sessionFile)
	if err == nil {
		return string(dt)
	}
	return
}

func noPrintFunc(opt map[string]Options) bool {
	for _, v := range opt {
		if v.PrintFunc != nil {
			return false
		}
	}
	return true
}
