package options

import (
	_ "crypto/sha256" // ensure digests can be computed
	"io"

	"github.com/docker/cli/opts"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/session"
	"github.com/moby/buildkit/util/entitlements"
	specs "github.com/opencontainers/image-spec/specs-go/v1"
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
