package build

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"github.com/containerd/containerd/v2/core/content"
	"github.com/containerd/containerd/v2/plugins/content/local"
	"github.com/containerd/platforms"
	"github.com/distribution/reference"
	"github.com/docker/buildx/builder"
	"github.com/docker/buildx/driver"
	"github.com/docker/buildx/util/confutil"
	"github.com/docker/buildx/util/dockerutil"
	"github.com/docker/buildx/util/osutil"
	"github.com/docker/buildx/util/progress"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/client/ociindex"
	gateway "github.com/moby/buildkit/frontend/gateway/client"
	"github.com/moby/buildkit/identity"
	"github.com/moby/buildkit/session/upload/uploadprovider"
	"github.com/moby/buildkit/solver/pb"
	"github.com/moby/buildkit/util/apicaps"
	"github.com/moby/buildkit/util/entitlements"
	"github.com/opencontainers/go-digest"
	"github.com/pkg/errors"
	"github.com/tonistiigi/fsutil"
)

func toSolveOpt(ctx context.Context, node builder.Node, multiDriver bool, opt *Options, bopts gateway.BuildOpts, cfg *confutil.Config, pw progress.Writer, docker *dockerutil.Client) (_ *client.SolveOpt, release func(), err error) {
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
		if e.Type != "inline" && !nodeDriver.Features(ctx)[driver.CacheExport] {
			return nil, nil, notSupported(driver.CacheExport, nodeDriver, "https://docs.docker.com/go/build-cache-backends/")
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
		Ref:                 opt.Ref,
		Frontend:            "dockerfile.v0",
		FrontendAttrs:       map[string]string{},
		LocalMounts:         map[string]fsutil.FS{},
		CacheExports:        cacheTo,
		CacheImports:        cacheFrom,
		AllowedEntitlements: opt.Allow,
		SourcePolicy:        opt.SourcePolicy,
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

	supportAttestations := bopts.LLBCaps.Contains(apicaps.CapID("exporter.image.attestations")) && nodeDriver.Features(ctx)[driver.MultiPlatform]
	if len(attests) > 0 {
		if !supportAttestations {
			if !nodeDriver.Features(ctx)[driver.MultiPlatform] {
				return nil, nil, notSupported("Attestation", nodeDriver, "https://docs.docker.com/go/attestations/")
			}
			return nil, nil, errors.Errorf("Attestations are not supported by the current BuildKit daemon")
		}
		for k, v := range attests {
			so.FrontendAttrs["attest:"+k] = v
		}
	}

	if _, ok := opt.Attests["provenance"]; !ok && supportAttestations {
		const noAttestEnv = "BUILDX_NO_DEFAULT_ATTESTATIONS"
		var noProv bool
		if v, ok := os.LookupEnv(noAttestEnv); ok {
			noProv, err = strconv.ParseBool(v)
			if err != nil {
				return nil, nil, errors.Wrap(err, "invalid "+noAttestEnv)
			}
		}
		if !noProv {
			so.FrontendAttrs["attest:provenance"] = "mode=min,inline-only=true"
		}
	}

	switch len(opt.Exports) {
	case 1:
		// valid
	case 0:
		if !noDefaultLoad() && opt.CallFunc == nil {
			if nodeDriver.IsMobyDriver() {
				// backwards compat for docker driver only:
				// this ensures the build results in a docker image.
				opt.Exports = []client.ExportEntry{{Type: "image", Attrs: map[string]string{}}}
			} else if nodeDriver.Features(ctx)[driver.DefaultLoad] {
				opt.Exports = []client.ExportEntry{{Type: "docker", Attrs: map[string]string{}}}
			}
		}
	default:
		if err := bopts.LLBCaps.Supports(pb.CapMultipleExporters); err != nil {
			return nil, nil, errors.Errorf("multiple outputs currently unsupported by the current BuildKit daemon, please upgrade to version v0.13+ or use a single output")
		}
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
		if e.Type == "oci" && !nodeDriver.Features(ctx)[driver.OCIExporter] {
			return nil, nil, notSupported(driver.OCIExporter, nodeDriver, "https://docs.docker.com/go/build-exporters/")
		}
		if e.Type == "docker" {
			features := docker.Features(ctx, e.Attrs["context"])
			if features[dockerutil.OCIImporter] && e.Output == nil {
				// rely on oci importer if available (which supports
				// multi-platform images), otherwise fall back to docker
				opt.Exports[i].Type = "oci"
			} else if len(opt.Platforms) > 1 || len(attests) > 0 {
				if e.Output != nil {
					return nil, nil, errors.Errorf("docker exporter does not support exporting manifest lists, use the oci exporter instead")
				}
				return nil, nil, errors.Errorf("docker exporter does not currently support exporting manifest lists")
			}
			if e.Output == nil {
				if nodeDriver.IsMobyDriver() {
					e.Type = "image"
				} else {
					w, cancel, err := docker.LoadImage(ctx, e.Attrs["context"], pw)
					if err != nil {
						return nil, nil, err
					}
					defers = append(defers, cancel)
					opt.Exports[i].Output = func(_ map[string]string) (io.WriteCloser, error) {
						return w, nil
					}
				}
			} else if !nodeDriver.Features(ctx)[driver.DockerExporter] {
				return nil, nil, notSupported(driver.DockerExporter, nodeDriver, "https://docs.docker.com/go/build-exporters/")
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
				opt.Exports[i].Attrs["buildinfo-attrs"] = v
			}
		}
	}

	so.Exports = opt.Exports
	so.Session = slices.Clone(opt.Session)

	releaseLoad, err := loadInputs(ctx, nodeDriver, &opt.Inputs, pw, &so)
	if err != nil {
		return nil, nil, err
	}
	defers = append(defers, releaseLoad)

	// add node identifier to shared key if one was specified
	if so.SharedKey != "" {
		so.SharedKey += ":" + cfg.TryNodeIdentifier()
	}

	if opt.Pull {
		so.FrontendAttrs["image-resolve-mode"] = pb.AttrImageResolveModeForcePull
	} else if nodeDriver.IsMobyDriver() {
		// moby driver always resolves local images by default
		so.FrontendAttrs["image-resolve-mode"] = pb.AttrImageResolveModePreferLocal
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

	// set platforms
	if len(opt.Platforms) != 0 {
		pp := make([]string, len(opt.Platforms))
		for i, p := range opt.Platforms {
			pp[i] = platforms.Format(p)
		}
		if len(pp) > 1 && !nodeDriver.Features(ctx)[driver.MultiPlatform] {
			return nil, nil, notSupported(driver.MultiPlatform, nodeDriver, "https://docs.docker.com/go/build-multi-platform/")
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
	extraHosts, err := toBuildkitExtraHosts(ctx, opt.ExtraHosts, nodeDriver)
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

	// mark call request as internal
	if opt.CallFunc != nil {
		so.Internal = true
	}

	return &so, releaseF, nil
}

func loadInputs(ctx context.Context, d *driver.DriverHandle, inp *Inputs, pw progress.Writer, target *client.SolveOpt) (func(), error) {
	if inp.ContextPath == "" {
		return nil, errors.New("please specify build context (e.g. \".\" for the current directory)")
	}

	// TODO: handle stdin, symlinks, remote contexts, check files exist

	var (
		err               error
		dockerfileReader  io.ReadCloser
		dockerfileDir     string
		dockerfileName    = inp.DockerfilePath
		dockerfileSrcName = inp.DockerfilePath
		toRemove          []string
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
			return nil, errors.Errorf("invalid argument: can't use stdin for both build context and dockerfile")
		}

		rc := inp.InStream.NewReadCloser()
		magic, err := inp.InStream.Peek(archiveHeaderSize * 2)
		if err != nil && err != io.EOF {
			return nil, errors.Wrap(err, "failed to peek context header from STDIN")
		}
		if !(err == io.EOF && len(magic) == 0) {
			if isArchive(magic) {
				// stdin is context
				up := uploadprovider.New()
				target.FrontendAttrs["context"] = up.Add(rc)
				target.Session = append(target.Session, up)
			} else {
				if inp.DockerfilePath != "" {
					return nil, errors.Errorf("ambiguous Dockerfile source: both stdin and flag correspond to Dockerfiles")
				}
				// stdin is dockerfile
				dockerfileReader = rc
				inp.ContextPath, _ = os.MkdirTemp("", "empty-dir")
				toRemove = append(toRemove, inp.ContextPath)
				if err := setLocalMount("context", inp.ContextPath, target); err != nil {
					return nil, err
				}
			}
		}
	case osutil.IsLocalDir(inp.ContextPath):
		if err := setLocalMount("context", inp.ContextPath, target); err != nil {
			return nil, err
		}
		sharedKey := inp.ContextPath
		if p, err := filepath.Abs(sharedKey); err == nil {
			sharedKey = filepath.Base(p)
		}
		target.SharedKey = sharedKey
		switch inp.DockerfilePath {
		case "-":
			dockerfileReader = inp.InStream.NewReadCloser()
		case "":
			dockerfileDir = inp.ContextPath
		default:
			dockerfileDir = filepath.Dir(inp.DockerfilePath)
			dockerfileName = filepath.Base(inp.DockerfilePath)
		}
	case IsRemoteURL(inp.ContextPath):
		if inp.DockerfilePath == "-" {
			dockerfileReader = inp.InStream.NewReadCloser()
		} else if filepath.IsAbs(inp.DockerfilePath) {
			dockerfileDir = filepath.Dir(inp.DockerfilePath)
			dockerfileName = filepath.Base(inp.DockerfilePath)
			target.FrontendAttrs["dockerfilekey"] = "dockerfile"
		}
		target.FrontendAttrs["context"] = inp.ContextPath
	default:
		return nil, errors.Errorf("unable to prepare context: path %q not found", inp.ContextPath)
	}

	if inp.DockerfileInline != "" {
		dockerfileReader = io.NopCloser(strings.NewReader(inp.DockerfileInline))
		dockerfileSrcName = "inline"
	} else if inp.DockerfilePath == "-" {
		dockerfileSrcName = "stdin"
	} else if inp.DockerfilePath == "" {
		dockerfileSrcName = filepath.Join(inp.ContextPath, "Dockerfile")
	}

	if dockerfileReader != nil {
		dockerfileDir, err = createTempDockerfile(dockerfileReader, inp.InStream)
		if err != nil {
			return nil, err
		}
		toRemove = append(toRemove, dockerfileDir)
		dockerfileName = "Dockerfile"
		target.FrontendAttrs["dockerfilekey"] = "dockerfile"
	}
	if isHTTPURL(inp.DockerfilePath) {
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
		if err := setLocalMount("dockerfile", dockerfileDir, target); err != nil {
			return nil, err
		}
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

		if IsRemoteURL(v.Path) || strings.HasPrefix(v.Path, "docker-image://") || strings.HasPrefix(v.Path, "target:") {
			target.FrontendAttrs["context:"+k] = v.Path
			continue
		}

		// handle OCI layout
		if strings.HasPrefix(v.Path, "oci-layout://") {
			localPath := strings.TrimPrefix(v.Path, "oci-layout://")
			localPath, dig, hasDigest := strings.Cut(localPath, "@")
			localPath, tag, hasTag := strings.Cut(localPath, ":")
			if !hasTag {
				tag = "latest"
			}
			if !hasDigest {
				dig, err = resolveDigest(localPath, tag)
				if err != nil {
					return nil, errors.Wrapf(err, "oci-layout reference %q could not be resolved", v.Path)
				}
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

			target.FrontendAttrs["context:"+k] = "oci-layout://" + storeName + ":" + tag + "@" + dig
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
		if err := setLocalMount(localName, v.Path, target); err != nil {
			return nil, err
		}
		target.FrontendAttrs["context:"+k] = "local:" + localName
	}

	release := func() {
		for _, dir := range toRemove {
			_ = os.RemoveAll(dir)
		}
	}

	inp.DockerfileMappingSrc = dockerfileSrcName
	inp.DockerfileMappingDst = dockerfileName
	return release, nil
}

func resolveDigest(localPath, tag string) (dig string, _ error) {
	idx := ociindex.NewStoreIndex(localPath)

	// lookup by name
	desc, err := idx.Get(tag)
	if err != nil {
		return "", err
	}
	if desc == nil {
		// lookup single
		desc, err = idx.GetSingle()
		if err != nil {
			return "", err
		}
	}
	if desc == nil {
		return "", errors.New("failed to resolve digest")
	}

	dig = string(desc.Digest)
	_, err = digest.Parse(dig)
	if err != nil {
		return "", errors.Wrapf(err, "invalid digest %s", dig)
	}

	return dig, nil
}

func setLocalMount(name, dir string, so *client.SolveOpt) error {
	lm, err := fsutil.NewFS(dir)
	if err != nil {
		return err
	}
	if so.LocalMounts == nil {
		so.LocalMounts = map[string]fsutil.FS{}
	}
	so.LocalMounts[name] = &fs{FS: lm, dir: dir}
	return nil
}

func createTempDockerfile(r io.Reader, multiReader *SyncMultiReader) (string, error) {
	dir, err := os.MkdirTemp("", "dockerfile")
	if err != nil {
		return "", err
	}
	f, err := os.Create(filepath.Join(dir, "Dockerfile"))
	if err != nil {
		return "", err
	}
	defer f.Close()

	if multiReader != nil {
		dt, err := io.ReadAll(r)
		if err != nil {
			return "", err
		}
		multiReader.Reset(dt)
		r = bytes.NewReader(dt)
	}

	if _, err := io.Copy(f, r); err != nil {
		return "", err
	}
	return dir, err
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

type fs struct {
	fsutil.FS
	dir string
}

var _ fsutil.FS = &fs{}
