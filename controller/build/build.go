package build

import (
	"context"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/docker/buildx/build"
	"github.com/docker/buildx/builder"
	controllerapi "github.com/docker/buildx/controller/pb"
	"github.com/docker/buildx/store"
	"github.com/docker/buildx/store/storeutil"
	"github.com/docker/buildx/util/buildflags"
	"github.com/docker/buildx/util/confutil"
	"github.com/docker/buildx/util/dockerutil"
	"github.com/docker/buildx/util/platformutil"
	"github.com/docker/buildx/util/progress"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/config"
	dockeropts "github.com/docker/cli/opts"
	"github.com/docker/go-units"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/session/auth/authprovider"
	"github.com/moby/buildkit/util/grpcerrors"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc/codes"
)

const defaultTargetName = "default"

// RunBuild runs the specified build and returns the result.
//
// NOTE: When an error happens during the build and this function acquires the debuggable *build.ResultHandle,
// this function returns it in addition to the error (i.e. it does "return nil, res, err"). The caller can
// inspect the result and debug the cause of that error.
func RunBuild(ctx context.Context, dockerCli command.Cli, in controllerapi.BuildOptions, inStream io.Reader, progress progress.Writer, generateResult bool) (*client.SolveResponse, *build.ResultHandle, error) {
	cResp, cRes, err := RunBuilds(ctx, dockerCli, map[string]controllerapi.BuildOptions{defaultTargetName: in}, inStream, progress, generateResult)
	var resp *client.SolveResponse
	if v, ok := cResp[defaultTargetName]; ok {
		resp = v
	}
	var res *build.ResultHandle
	if v, ok := cRes[defaultTargetName]; ok {
		res = v
	}
	return resp, res, err
}

// RunBuilds same as RunBuild but runs multiple builds.
func RunBuilds(ctx context.Context, dockerCli command.Cli, in map[string]controllerapi.BuildOptions, inStream io.Reader, progress progress.Writer, generateResult bool) (map[string]*client.SolveResponse, map[string]*build.ResultHandle, error) {
	var err error
	var builderName string
	var contextPathHash string

	opts := make(map[string]build.Options, len(in))
	mu := sync.Mutex{}
	eg, _ := errgroup.WithContext(ctx)
	for t, o := range in {
		func(t string, o controllerapi.BuildOptions) {
			eg.Go(func() error {
				opt, err := ToBuildOpts(o, inStream)
				if err != nil {
					return err
				}
				mu.Lock()
				opts[t] = *opt
				// we assume that all the targets are using the same builder and
				// context path hash. This assumption is currently valid but, we
				// may need to revisit this in the future.
				if builderName == "" {
					builderName = o.Opts.Builder
				}
				if contextPathHash == "" {
					contextPathHash = o.Inputs.ContextPathHash
				}
				mu.Unlock()
				return nil
			})
		}(t, o)
	}
	if err := eg.Wait(); err != nil {
		return nil, nil, err
	}

	b, err := builder.New(dockerCli,
		builder.WithName(builderName),
		builder.WithContextPathHash(contextPathHash),
	)
	if err != nil {
		return nil, nil, err
	}
	if err = updateLastActivity(dockerCli, b.NodeGroup); err != nil {
		return nil, nil, errors.Wrapf(err, "failed to update builder last activity time")
	}
	nodes, err := b.LoadNodes(ctx, false)
	if err != nil {
		return nil, nil, err
	}

	resp, res, err := buildTargets(ctx, dockerCli, b.NodeGroup, nodes, opts, progress, generateResult)
	err = wrapBuildError(err, false)
	if err != nil {
		return nil, nil, err
	}
	return resp, res, nil
}

func ToBuildOpts(in controllerapi.BuildOptions, inStream io.Reader) (*build.Options, error) {
	if in.Opts.NoCache && len(in.NoCacheFilter) > 0 {
		return nil, errors.Errorf("--no-cache and --no-cache-filter cannot currently be used together")
	}

	var ctxst *llb.State
	if in.Inputs.ContextDefinition != nil {
		defop, err := llb.NewDefinitionOp(in.Inputs.ContextDefinition)
		if err != nil {
			return nil, err
		}
		st := llb.NewState(defop)
		ctxst = &st
	}

	contexts := map[string]build.NamedContext{}
	for name, nctx := range in.Inputs.NamedContexts {
		if nctx.Definition != nil {
			defop, err := llb.NewDefinitionOp(nctx.Definition)
			if err != nil {
				return nil, err
			}
			st := llb.NewState(defop)
			contexts[name] = build.NamedContext{State: &st}
		} else {
			contexts[name] = build.NamedContext{Path: nctx.Path}
		}
	}

	opts := build.Options{
		Inputs: build.Inputs{
			ContextPath:      in.Inputs.ContextPath,
			DockerfilePath:   in.Inputs.DockerfileName,
			DockerfileInline: in.Inputs.DockerfileInline,
			ContextState:     ctxst,
			InStream:         inStream,
			NamedContexts:    contexts,
		},
		BuildArgs:     in.BuildArgs,
		ExtraHosts:    in.ExtraHosts,
		Labels:        in.Labels,
		NetworkMode:   in.NetworkMode,
		NoCache:       in.Opts.NoCache,
		NoCacheFilter: in.NoCacheFilter,
		Pull:          in.Opts.Pull,
		ShmSize:       dockeropts.MemBytes(in.ShmSize),
		Tags:          in.Tags,
		Target:        in.Target,
		Ulimits:       controllerUlimitOpt2DockerUlimit(in.Ulimits),
	}

	platforms, err := platformutil.Parse(in.Platforms)
	if err != nil {
		return nil, err
	}
	opts.Platforms = platforms

	dockerConfig := config.LoadDefaultConfigFile(os.Stderr)
	opts.Session = append(opts.Session, authprovider.NewDockerAuthProvider(dockerConfig))

	secrets, err := controllerapi.CreateSecrets(in.Secrets)
	if err != nil {
		return nil, err
	}
	opts.Session = append(opts.Session, secrets)

	sshSpecs := in.SSH
	if len(sshSpecs) == 0 && buildflags.IsGitSSH(in.Inputs.ContextPath) {
		sshSpecs = append(sshSpecs, &controllerapi.SSH{ID: "default"})
	}
	ssh, err := controllerapi.CreateSSH(sshSpecs)
	if err != nil {
		return nil, err
	}
	opts.Session = append(opts.Session, ssh)

	outputs, err := controllerapi.CreateExports(in.Exports)
	if err != nil {
		return nil, err
	}
	if in.Opts.ExportPush {
		if in.Opts.ExportLoad {
			return nil, errors.Errorf("push and load may not be set together at the moment")
		}
		if len(outputs) == 0 {
			outputs = []client.ExportEntry{{
				Type: "image",
				Attrs: map[string]string{
					"push": "true",
				},
			}}
		} else {
			switch outputs[0].Type {
			case "image":
				outputs[0].Attrs["push"] = "true"
			default:
				return nil, errors.Errorf("push and %q output can't be used together", outputs[0].Type)
			}
		}
	}
	if in.Opts.ExportLoad {
		if len(outputs) == 0 {
			outputs = []client.ExportEntry{{
				Type:  "docker",
				Attrs: map[string]string{},
			}}
		} else {
			switch outputs[0].Type {
			case "docker":
			default:
				return nil, errors.Errorf("load and %q output can't be used together", outputs[0].Type)
			}
		}
	}
	opts.Exports = outputs

	opts.CacheFrom = controllerapi.CreateCaches(in.CacheFrom)
	opts.CacheTo = controllerapi.CreateCaches(in.CacheTo)

	opts.Attests = controllerapi.CreateAttestations(in.Attests)

	opts.SourcePolicy = in.SourcePolicy

	allow, err := buildflags.ParseEntitlements(in.Allow)
	if err != nil {
		return nil, err
	}
	opts.Allow = allow

	if in.PrintFunc != nil {
		opts.PrintFunc = &build.PrintFunc{
			Name:   in.PrintFunc.Name,
			Format: in.PrintFunc.Format,
		}
	}

	return &opts, nil
}

// buildTargets runs the specified build and returns the result.
//
// NOTE: When an error happens during the build and this function acquires the debuggable *build.ResultHandle,
// this function returns it in addition to the error (i.e. it does "return nil, res, err"). The caller can
// inspect the result and debug the cause of that error.
func buildTargets(ctx context.Context, dockerCli command.Cli, ng *store.NodeGroup, nodes []builder.Node, opts map[string]build.Options, progress progress.Writer, generateResult bool) (map[string]*client.SolveResponse, map[string]*build.ResultHandle, error) {
	var res map[string]*build.ResultHandle
	var resp map[string]*client.SolveResponse
	var err error
	if generateResult {
		var mu sync.Mutex
		resp, err = build.BuildWithResultHandler(ctx, nodes, opts, dockerutil.NewClient(dockerCli), confutil.ConfigDir(dockerCli), progress, func(target string, gotRes *build.ResultHandle) {
			mu.Lock()
			defer mu.Unlock()
			if res == nil {
				res = make(map[string]*build.ResultHandle)
			}
			res[target] = gotRes
		})
	} else {
		resp, err = build.Build(ctx, nodes, opts, dockerutil.NewClient(dockerCli), confutil.ConfigDir(dockerCli), progress)
	}
	if err != nil {
		return nil, res, err
	}
	return resp, res, err
}

func wrapBuildError(err error, bake bool) error {
	if err == nil {
		return nil
	}
	st, ok := grpcerrors.AsGRPCStatus(err)
	if ok {
		if st.Code() == codes.Unimplemented && strings.Contains(st.Message(), "unsupported frontend capability moby.buildkit.frontend.contexts") {
			msg := "current frontend does not support --build-context."
			if bake {
				msg = "current frontend does not support defining additional contexts for targets."
			}
			msg += " Named contexts are supported since Dockerfile v1.4. Use #syntax directive in Dockerfile or update to latest BuildKit."
			return &wrapped{err, msg}
		}
	}
	return err
}

type wrapped struct {
	err error
	msg string
}

func (w *wrapped) Error() string {
	return w.msg
}

func (w *wrapped) Unwrap() error {
	return w.err
}

func updateLastActivity(dockerCli command.Cli, ng *store.NodeGroup) error {
	txn, release, err := storeutil.GetStore(dockerCli)
	if err != nil {
		return err
	}
	defer release()
	return txn.UpdateLastActivity(ng)
}

func controllerUlimitOpt2DockerUlimit(u *controllerapi.UlimitOpt) *dockeropts.UlimitOpt {
	if u == nil {
		return nil
	}
	values := make(map[string]*units.Ulimit)
	for k, v := range u.Values {
		values[k] = &units.Ulimit{
			Name: v.Name,
			Hard: v.Hard,
			Soft: v.Soft,
		}
	}
	return dockeropts.NewUlimitOpt(&values)
}
