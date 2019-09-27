package build

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/containerd/containerd/images"
	"github.com/containerd/containerd/platforms"
	"github.com/docker/buildx/driver"
	"github.com/docker/buildx/util/imagetools"
	"github.com/docker/buildx/util/progress"
	clitypes "github.com/docker/cli/cli/config/types"
	"github.com/docker/distribution/reference"
	dockerclient "github.com/docker/docker/client"
	"github.com/docker/docker/pkg/urlutil"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/session"
	"github.com/moby/buildkit/session/upload/uploadprovider"
	"github.com/moby/buildkit/util/entitlements"
	"github.com/opencontainers/go-digest"
	specs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
)

var (
	errStdinConflict      = errors.New("invalid argument: can't use stdin for both build context and dockerfile")
	errDockerfileConflict = errors.New("ambiguous Dockerfile source: both stdin and flag correspond to Dockerfiles")
)

type Options struct {
	Inputs      Inputs
	Tags        []string
	Labels      map[string]string
	BuildArgs   map[string]string
	Pull        bool
	ImageIDFile string
	ExtraHosts  []string
	NetworkMode string

	NoCache   bool
	Target    string
	Platforms []specs.Platform
	Exports   []client.ExportEntry
	Session   []session.Attachable

	CacheFrom []client.CacheOptionsEntry
	CacheTo   []client.CacheOptionsEntry

	Allow []entitlements.Entitlement
	// DockerTarget
}

type Inputs struct {
	ContextPath    string
	DockerfilePath string
	InStream       io.Reader
}

type DriverInfo struct {
	Driver   driver.Driver
	Name     string
	Platform []specs.Platform
	Err      error
}

type Auth interface {
	GetAuthConfig(registryHostname string) (clitypes.AuthConfig, error)
}

type DockerAPI interface {
	DockerAPI(name string) (dockerclient.APIClient, error)
}

func filterAvailableDrivers(drivers []DriverInfo) ([]DriverInfo, error) {
	out := make([]DriverInfo, 0, len(drivers))
	err := errors.Errorf("no drivers found")
	for _, di := range drivers {
		if di.Err == nil && di.Driver != nil {
			out = append(out, di)
		}
		if di.Err != nil {
			err = di.Err
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

func ensureBooted(ctx context.Context, drivers []DriverInfo, idxs []int, pw progress.Writer) ([]*client.Client, error) {
	clients := make([]*client.Client, len(drivers))

	eg, ctx := errgroup.WithContext(ctx)

	for _, i := range idxs {
		func(i int) {
			eg.Go(func() error {
				c, err := driver.Boot(ctx, drivers[i].Driver, pw)
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
		dps := make([]driverPair, 0, 2)
		for idx, pp := range mm {
			dps = append(dps, driverPair{driverIndex: idx, platforms: pp})
		}
		m[k] = dps
	}
	return m
}

func resolveDrivers(ctx context.Context, drivers []DriverInfo, opt map[string]Options, pw progress.Writer) (map[string][]driverPair, []*client.Client, error) {

	availablePlatforms := map[string]int{}
	for i, d := range drivers {
		for _, p := range d.Platform {
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
	if len(drivers) == 1 || len(allPlatforms) == 0 {
		m := map[string][]driverPair{}
		for k, opt := range opt {
			m[k] = []driverPair{{driverIndex: 0, platforms: opt.Platforms}}
		}
		clients, err := ensureBooted(ctx, drivers, driverIndexes(m), pw)
		if err != nil {
			return nil, nil, err
		}
		return m, clients, nil
	}

	// map based on existing platforms
	if !undetectedPlatform {
		m := splitToDriverPairs(availablePlatforms, opt)
		clients, err := ensureBooted(ctx, drivers, driverIndexes(m), pw)
		if err != nil {
			return nil, nil, err
		}
		return m, clients, nil
	}

	// boot all drivers in k
	clients, err := ensureBooted(ctx, drivers, allIndexes(len(drivers)), pw)
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

func isDefaultMobyDriver(d driver.Driver) bool {
	_, ok := d.(interface {
		IsDefaultMobyDriver()
	})
	return ok
}

func toSolveOpt(d driver.Driver, multiDriver bool, opt Options, dl dockerLoadCallback) (solveOpt *client.SolveOpt, release func(), err error) {
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
		if multiDriver || len(opt.Platforms) != 0 {
			return nil, nil, errors.Errorf("image ID file cannot be specified when building for multiple platforms")
		}
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
		if e.Type != "inline" && !d.Features()[driver.CacheExport] {
			return nil, nil, notSupported(d, driver.CacheExport)
		}
	}

	so := client.SolveOpt{
		Frontend:            "dockerfile.v0",
		FrontendAttrs:       map[string]string{},
		LocalDirs:           map[string]string{},
		CacheExports:        opt.CacheTo,
		CacheImports:        opt.CacheFrom,
		AllowedEntitlements: opt.Allow,
	}

	if multiDriver {
		// force creation of manifest list
		so.FrontendAttrs["multi-platform"] = "true"
	}

	_, isDefaultMobyDriver := d.(interface {
		IsDefaultMobyDriver()
	})

	switch len(opt.Exports) {
	case 1:
		// valid
	case 0:
		if isDefaultMobyDriver {
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

	// set up exporters
	for i, e := range opt.Exports {
		if (e.Type == "local" || e.Type == "tar") && opt.ImageIDFile != "" {
			return nil, nil, errors.Errorf("local and tar exporters are incompatible with image ID file")
		}
		if e.Type == "oci" && !d.Features()[driver.OCIExporter] {
			return nil, nil, notSupported(d, driver.OCIExporter)
		}
		if e.Type == "docker" {
			if e.Output == nil {
				if isDefaultMobyDriver {
					e.Type = "image"
				} else {
					w, cancel, err := dl(e.Attrs["context"])
					if err != nil {
						return nil, nil, err
					}
					defers = append(defers, cancel)
					opt.Exports[i].Output = wrapWriteCloser(w)
				}
			} else if !d.Features()[driver.DockerExporter] {
				return nil, nil, notSupported(d, driver.DockerExporter)
			}
		}
		if e.Type == "image" && isDefaultMobyDriver {
			opt.Exports[i].Type = "moby"
			if e.Attrs["push"] != "" {
				if ok, _ := strconv.ParseBool(e.Attrs["push"]); ok {
					return nil, nil, errors.Errorf("auto-push is currently not implemented for docker driver")
				}
			}
		}
	}

	so.Exports = opt.Exports
	so.Session = opt.Session

	releaseLoad, err := LoadInputs(opt.Inputs, &so)
	if err != nil {
		return nil, nil, err
	}
	defers = append(defers, releaseLoad)

	if opt.Pull {
		so.FrontendAttrs["image-resolve-mode"] = "pull"
	}
	if opt.Target != "" {
		so.FrontendAttrs["target"] = opt.Target
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

	// set platforms
	if len(opt.Platforms) != 0 {
		pp := make([]string, len(opt.Platforms))
		for i, p := range opt.Platforms {
			pp[i] = platforms.Format(p)
		}
		if len(pp) > 1 && !d.Features()[driver.MultiPlatform] {
			return nil, nil, notSupported(d, driver.MultiPlatform)
		}
		so.FrontendAttrs["platform"] = strings.Join(pp, ",")
	}

	// setup networkmode
	switch opt.NetworkMode {
	case "host", "none":
		so.FrontendAttrs["force-network-mode"] = opt.NetworkMode
		so.AllowedEntitlements = append(so.AllowedEntitlements, entitlements.EntitlementNetworkHost)
	case "", "default":
	default:
		return nil, nil, errors.Errorf("network mode %q not supported by buildkit", opt.NetworkMode)
	}

	// setup extrahosts
	extraHosts, err := toBuildkitExtraHosts(opt.ExtraHosts)
	if err != nil {
		return nil, nil, err
	}
	so.FrontendAttrs["add-hosts"] = extraHosts

	return &so, releaseF, nil
}

func Build(ctx context.Context, drivers []DriverInfo, opt map[string]Options, docker DockerAPI, auth Auth, pw progress.Writer) (resp map[string]*client.SolveResponse, err error) {
	if len(drivers) == 0 {
		return nil, errors.Errorf("driver required for build")
	}

	drivers, err = filterAvailableDrivers(drivers)
	if err != nil {
		return nil, errors.Wrapf(err, "no valid drivers found")
	}

	var noMobyDriver driver.Driver
	for _, d := range drivers {
		if !isDefaultMobyDriver(d.Driver) {
			noMobyDriver = d.Driver
			break
		}
	}

	if noMobyDriver != nil {
		for _, opt := range opt {
			if len(opt.Exports) == 0 {
				logrus.Warnf("No output specified for %s driver. Build result will only remain in the build cache. To push result image into registry use --push or to load image into docker use --load", noMobyDriver.Factory().Name())
				break
			}
		}
	}

	m, clients, err := resolveDrivers(ctx, drivers, opt, pw)
	if err != nil {
		close(pw.Status())
		<-pw.Done()
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

	mw := progress.NewMultiWriter(pw)
	eg, ctx := errgroup.WithContext(ctx)

	for k, opt := range opt {
		multiDriver := len(m[k]) > 1
		for i, dp := range m[k] {
			d := drivers[dp.driverIndex].Driver
			opt.Platforms = dp.platforms
			so, release, err := toSolveOpt(d, multiDriver, opt, func(name string) (io.WriteCloser, func(), error) {
				return newDockerLoader(ctx, docker, name, mw)
			})
			if err != nil {
				return nil, err
			}
			defers = append(defers, release)
			m[k][i].so = so
		}
	}

	resp = map[string]*client.SolveResponse{}
	var respMu sync.Mutex

	multiTarget := len(opt) > 1

	for k, opt := range opt {
		err := func(k string) error {
			opt := opt
			dps := m[k]
			multiDriver := len(m[k]) > 1

			res := make([]*client.SolveResponse, len(dps))
			wg := &sync.WaitGroup{}
			wg.Add(len(dps))

			var pushNames string

			eg.Go(func() error {
				pw := mw.WithPrefix("default", false)
				defer close(pw.Status())
				wg.Wait()
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
				}

				respMu.Lock()
				resp[k] = res[0]
				respMu.Unlock()
				if len(res) == 1 {
					if opt.ImageIDFile != "" {
						return ioutil.WriteFile(opt.ImageIDFile, []byte(res[0].ExporterResponse["containerimage.digest"]), 0644)
					}
					return nil
				}

				if pushNames != "" {
					progress.Write(pw, fmt.Sprintf("merging manifest list %s", pushNames), func() error {
						descs := make([]specs.Descriptor, 0, len(res))

						for _, r := range res {
							s, ok := r.ExporterResponse["containerimage.digest"]
							if ok {
								descs = append(descs, specs.Descriptor{
									Digest:    digest.Digest(s),
									MediaType: images.MediaTypeDockerSchema2ManifestList,
									Size:      -1,
								})
							}
						}
						if len(descs) > 0 {
							itpull := imagetools.New(imagetools.Opt{
								Auth: auth,
							})

							names := strings.Split(pushNames, ",")
							dt, desc, err := itpull.Combine(ctx, names[0], descs)
							if err != nil {
								return err
							}
							if opt.ImageIDFile != "" {
								return ioutil.WriteFile(opt.ImageIDFile, []byte(desc.Digest), 0644)
							}

							itpush := imagetools.New(imagetools.Opt{
								Auth: auth,
							})

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

			for i, dp := range dps {
				so := *dp.so

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
									e.Attrs["name"] = names
									e.Attrs["push-by-digest"] = "true"
									so.Exports[i].Attrs = e.Attrs
								}
							}
						}
					}
				}

				func(i int, dp driverPair, so client.SolveOpt) {
					pw := mw.WithPrefix(k, multiTarget)

					c := clients[dp.driverIndex]

					var statusCh chan *client.SolveStatus
					if pw != nil {
						pw = progress.ResetTime(pw)
						statusCh = pw.Status()
						eg.Go(func() error {
							<-pw.Done()
							return pw.Err()
						})
					}

					eg.Go(func() error {
						defer wg.Done()
						rr, err := c.Solve(ctx, nil, so, statusCh)
						if err != nil {
							return err
						}
						res[i] = rr
						return nil
					})

				}(i, dp, so)
			}

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

func createTempDockerfile(r io.Reader) (string, error) {
	dir, err := ioutil.TempDir("", "dockerfile")
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

func LoadInputs(inp Inputs, target *client.SolveOpt) (func(), error) {
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
	case inp.ContextPath == "-":
		if inp.DockerfilePath == "-" {
			return nil, errStdinConflict
		}

		buf := bufio.NewReader(inp.InStream)
		magic, err := buf.Peek(archiveHeaderSize * 2)
		if err != nil && err != io.EOF {
			return nil, errors.Wrap(err, "failed to peek context header from STDIN")
		}

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
			inp.ContextPath, _ = ioutil.TempDir("", "empty-dir")
			toRemove = append(toRemove, inp.ContextPath)
			target.LocalDirs["context"] = inp.ContextPath
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
			return nil, errors.Errorf("Dockerfile from stdin is not supported with remote contexts")
		}
		target.FrontendAttrs["context"] = inp.ContextPath
	default:
		return nil, errors.Errorf("unable to prepare context: path %q not found", inp.ContextPath)
	}

	if dockerfileReader != nil {
		dockerfileDir, err = createTempDockerfile(dockerfileReader)
		if err != nil {
			return nil, err
		}
		toRemove = append(toRemove, dockerfileDir)
		dockerfileName = "Dockerfile"
	}

	if dockerfileName == "" {
		dockerfileName = "Dockerfile"
	}
	target.FrontendAttrs["filename"] = dockerfileName

	if dockerfileDir != "" {
		target.LocalDirs["dockerfile"] = dockerfileDir
	}

	release := func() {
		for _, dir := range toRemove {
			os.RemoveAll(dir)
		}
	}
	return release, nil
}

func notSupported(d driver.Driver, f driver.Feature) error {
	return errors.Errorf("%s feature is currently not supported for %s driver. Please switch to a different driver (eg. \"docker buildx create --use\")", f, d.Factory().Name())
}

type dockerLoadCallback func(name string) (io.WriteCloser, func(), error)

func newDockerLoader(ctx context.Context, d DockerAPI, name string, mw *progress.MultiWriter) (io.WriteCloser, func(), error) {
	c, err := d.DockerAPI(name)
	if err != nil {
		return nil, nil, err
	}

	pr, pw := io.Pipe()
	started := make(chan struct{})
	w := &waitingWriter{
		PipeWriter: pw,
		f: func() {
			resp, err := c.ImageLoad(ctx, pr, false)
			if err != nil {
				pr.CloseWithError(err)
				return
			}
			prog := mw.WithPrefix("", false)
			close(started)
			progress.FromReader(prog, "importing to docker", resp.Body)
		},
		started: started,
	}
	return w, func() {
		pr.Close()
	}, nil
}

type waitingWriter struct {
	*io.PipeWriter
	f       func()
	once    sync.Once
	mu      sync.Mutex
	err     error
	started chan struct{}
}

func (w *waitingWriter) Write(dt []byte) (int, error) {
	w.once.Do(func() {
		go w.f()
	})
	return w.PipeWriter.Write(dt)
}

func (w *waitingWriter) Close() error {
	err := w.PipeWriter.Close()
	<-w.started
	return err
}
