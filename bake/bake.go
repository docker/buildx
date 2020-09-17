package bake

import (
	"context"
	"io/ioutil"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/docker/buildx/build"
	"github.com/docker/buildx/util/platformutil"
	"github.com/docker/docker/pkg/urlutil"
	hcl "github.com/hashicorp/hcl/v2"
	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/session/auth/authprovider"
	"github.com/pkg/errors"
)

var httpPrefix = regexp.MustCompile(`^https?://`)
var gitURLPathWithFragmentSuffix = regexp.MustCompile(`\.git(?:#.+)?$`)

type File struct {
	Name string
	Data []byte
}

func defaultFilenames() []string {
	return []string{
		"docker-compose.yml",  // support app
		"docker-compose.yaml", // support app
		"docker-bake.json",
		"docker-bake.override.json",
		"docker-bake.hcl",
		"docker-bake.override.hcl",
	}
}

func ReadLocalFiles(names []string) ([]File, error) {
	isDefault := false
	if len(names) == 0 {
		isDefault = true
		names = defaultFilenames()
	}
	out := make([]File, 0, len(names))

	for _, n := range names {
		dt, err := ioutil.ReadFile(n)
		if err != nil {
			if isDefault && errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}
		out = append(out, File{Name: n, Data: dt})
	}
	return out, nil
}

func ReadTargets(ctx context.Context, files []File, targets, overrides []string) (map[string]*Target, error) {
	var c Config
	for _, f := range files {
		cfg, err := ParseFile(f.Data, f.Name)
		if err != nil {
			return nil, err
		}
		c = mergeConfig(c, *cfg)
	}
	o, err := c.newOverrides(overrides)
	if err != nil {
		return nil, err
	}
	m := map[string]*Target{}
	for _, n := range targets {
		for _, n := range c.ResolveGroup(n) {
			t, err := c.ResolveTarget(n, o)
			if err != nil {
				return nil, err
			}
			if t != nil {
				m[n] = t
			}
		}
	}
	return m, nil
}

func ParseFile(dt []byte, fn string) (*Config, error) {
	fnl := strings.ToLower(fn)
	if strings.HasSuffix(fnl, ".yml") || strings.HasSuffix(fnl, ".yaml") {
		return ParseCompose(dt)
	}

	if strings.HasSuffix(fnl, ".json") || strings.HasSuffix(fnl, ".hcl") {
		return ParseHCL(dt, fn)
	}

	cfg, err := ParseCompose(dt)
	if err != nil {
		cfg, err2 := ParseHCL(dt, fn)
		if err2 != nil {
			return nil, errors.Errorf("failed to parse %s: parsing yaml: %s, parsing hcl: %s", fn, err.Error(), err2.Error())
		}
		return cfg, nil
	}
	return cfg, nil
}

type Config struct {
	Variables []*Variable `json:"-" hcl:"variable,block"`
	Groups    []*Group    `json:"group" hcl:"group,block"`
	Targets   []*Target   `json:"target" hcl:"target,block"`
	Remain    hcl.Body    `json:"-" hcl:",remain"`
}

func mergeConfig(c1, c2 Config) Config {
	if c1.Groups == nil {
		c1.Groups = []*Group{}
	}

	for _, g2 := range c2.Groups {
		var g1 *Group
		for _, g := range c1.Groups {
			if g2.Name == g.Name {
				g1 = g
				break
			}
		}
		if g1 == nil {
			c1.Groups = append(c1.Groups, g2)
			continue
		}

	nextTarget:
		for _, t2 := range g2.Targets {
			for _, t1 := range g1.Targets {
				if t1 == t2 {
					continue nextTarget
				}
			}
			g1.Targets = append(g1.Targets, t2)
		}
		c1.Groups = append(c1.Groups, g1)
	}

	if c1.Targets == nil {
		c1.Targets = []*Target{}
	}

	for _, t2 := range c2.Targets {
		var t1 *Target
		for _, t := range c1.Targets {
			if t2.Name == t.Name {
				t1 = t
				break
			}
		}
		if t1 != nil {
			t2 = merge(t1, t2)
		}
		c1.Targets = append(c1.Targets, t2)
	}

	return c1
}

func (c Config) expandTargets(pattern string) ([]string, error) {
	for _, target := range c.Targets {
		if target.Name == pattern {
			return []string{pattern}, nil
		}
	}

	var names []string
	for _, target := range c.Targets {
		ok, err := path.Match(pattern, target.Name)
		if err != nil {
			return nil, errors.Wrapf(err, "could not match targets with '%s'", pattern)
		}
		if ok {
			names = append(names, target.Name)
		}
	}
	if len(names) == 0 {
		return nil, errors.Errorf("could not find any target matching '%s'", pattern)
	}
	return names, nil
}

func (c Config) newOverrides(v []string) (map[string]*Target, error) {
	m := map[string]*Target{}
	for _, v := range v {

		parts := strings.SplitN(v, "=", 2)
		keys := strings.SplitN(parts[0], ".", 3)
		if len(keys) < 2 {
			return nil, errors.Errorf("invalid override key %s, expected target.name", parts[0])
		}

		pattern := keys[0]
		if len(parts) != 2 && keys[1] != "args" {
			return nil, errors.Errorf("invalid override %s, expected target.name=value", v)
		}

		names, err := c.expandTargets(pattern)
		if err != nil {
			return nil, err
		}

		for _, name := range names {
			t, ok := m[name]
			if !ok {
				t = &Target{}
			}

			switch keys[1] {
			case "context":
				t.Context = &parts[1]
			case "dockerfile":
				t.Dockerfile = &parts[1]
			case "args":
				if len(keys) != 3 {
					return nil, errors.Errorf("invalid key %s, args requires name", parts[0])
				}
				if t.Args == nil {
					t.Args = map[string]string{}
				}
				if len(parts) < 2 {
					v, ok := os.LookupEnv(keys[2])
					if ok {
						t.Args[keys[2]] = v
					}
				} else {
					t.Args[keys[2]] = parts[1]
				}
			case "labels":
				if len(keys) != 3 {
					return nil, errors.Errorf("invalid key %s, lanels requires name", parts[0])
				}
				if t.Labels == nil {
					t.Labels = map[string]string{}
				}
				t.Labels[keys[2]] = parts[1]
			case "tags":
				t.Tags = append(t.Tags, parts[1])
			case "cache-from":
				t.CacheFrom = append(t.CacheFrom, parts[1])
			case "cache-to":
				t.CacheTo = append(t.CacheTo, parts[1])
			case "target":
				s := parts[1]
				t.Target = &s
			case "secrets":
				t.Secrets = append(t.Secrets, parts[1])
			case "ssh":
				t.SSH = append(t.SSH, parts[1])
			case "platform":
				t.Platforms = append(t.Platforms, parts[1])
			case "output":
				t.Outputs = append(t.Outputs, parts[1])
			case "no-cache":
				noCache, err := strconv.ParseBool(parts[1])
				if err != nil {
					return nil, errors.Errorf("invalid value %s for boolean key no-cache", parts[1])
				}
				t.NoCache = &noCache
			case "pull":
				pull, err := strconv.ParseBool(parts[1])
				if err != nil {
					return nil, errors.Errorf("invalid value %s for boolean key pull", parts[1])
				}
				t.Pull = &pull
			default:
				return nil, errors.Errorf("unknown key: %s", keys[1])
			}
			m[name] = t
		}
	}
	return m, nil
}

func (c Config) ResolveGroup(name string) []string {
	return c.group(name, map[string]struct{}{})
}

func (c Config) group(name string, visited map[string]struct{}) []string {
	if _, ok := visited[name]; ok {
		return nil
	}
	var g *Group
	for _, group := range c.Groups {
		if group.Name == name {
			g = group
			break
		}
	}
	if g == nil {
		return []string{name}
	}
	visited[name] = struct{}{}
	targets := make([]string, 0, len(g.Targets))
	for _, t := range g.Targets {
		targets = append(targets, c.group(t, visited)...)
	}
	return targets
}

func (c Config) ResolveTarget(name string, overrides map[string]*Target) (*Target, error) {
	t, err := c.target(name, map[string]struct{}{}, overrides)
	if err != nil {
		return nil, err
	}
	if t.Context == nil {
		s := "."
		t.Context = &s
	}
	if t.Dockerfile == nil {
		s := "Dockerfile"
		t.Dockerfile = &s
	}
	return t, nil
}

func (c Config) target(name string, visited map[string]struct{}, overrides map[string]*Target) (*Target, error) {
	if _, ok := visited[name]; ok {
		return nil, nil
	}
	visited[name] = struct{}{}
	var t *Target
	for _, target := range c.Targets {
		if target.Name == name {
			t = target
			break
		}
	}
	if t == nil {
		return nil, errors.Errorf("failed to find target %s", name)
	}
	tt := &Target{}
	for _, name := range t.Inherits {
		t, err := c.target(name, visited, overrides)
		if err != nil {
			return nil, err
		}
		if t != nil {
			tt = merge(tt, t)
		}
	}
	t.Inherits = nil
	tt = merge(merge(defaultTarget(), tt), t)
	if override, ok := overrides[name]; ok {
		tt = merge(tt, override)
	}
	tt.normalize()
	return tt, nil
}

type Variable struct {
	Name    string `json:"-" hcl:"name,label"`
	Default string `json:"default,omitempty" hcl:"default,optional"`
}

type Group struct {
	Name    string   `json:"-" hcl:"name,label"`
	Targets []string `json:"targets" hcl:"targets"`
	// Target // TODO?
}

type Target struct {
	Name string `json:"-" hcl:"name,label"`

	// Inherits is the only field that cannot be overridden with --set
	Inherits []string `json:"inherits,omitempty" hcl:"inherits,optional"`

	Context    *string           `json:"context,omitempty" hcl:"context,optional"`
	Dockerfile *string           `json:"dockerfile,omitempty" hcl:"dockerfile,optional"`
	Args       map[string]string `json:"args,omitempty" hcl:"args,optional"`
	Labels     map[string]string `json:"labels,omitempty" hcl:"labels,optional"`
	Tags       []string          `json:"tags,omitempty" hcl:"tags,optional"`
	CacheFrom  []string          `json:"cache-from,omitempty"  hcl:"cache-from,optional"`
	CacheTo    []string          `json:"cache-to,omitempty"  hcl:"cache-to,optional"`
	Target     *string           `json:"target,omitempty" hcl:"target,optional"`
	Secrets    []string          `json:"secret,omitempty" hcl:"secret,optional"`
	SSH        []string          `json:"ssh,omitempty" hcl:"ssh,optional"`
	Platforms  []string          `json:"platforms,omitempty" hcl:"platforms,optional"`
	Outputs    []string          `json:"output,omitempty" hcl:"output,optional"`
	Pull       *bool             `json:"pull,omitempty" hcl:"pull,optional"`
	NoCache    *bool             `json:"no-cache,omitempty" hcl:"no-cache,optional"`

	// IMPORTANT: if you add more fields here, do not forget to update newOverrides and README.
}

func (t *Target) normalize() {
	t.Tags = removeDupes(t.Tags)
	t.Secrets = removeDupes(t.Secrets)
	t.SSH = removeDupes(t.SSH)
	t.Platforms = removeDupes(t.Platforms)
	t.CacheFrom = removeDupes(t.CacheFrom)
	t.CacheTo = removeDupes(t.CacheTo)
	t.Outputs = removeDupes(t.Outputs)
}

func TargetsToBuildOpt(m map[string]*Target, inp *Input) (map[string]build.Options, error) {
	m2 := make(map[string]build.Options, len(m))
	for k, v := range m {
		bo, err := toBuildOpt(v, inp)
		if err != nil {
			return nil, err
		}
		m2[k] = *bo
	}
	return m2, nil
}

func updateContext(t *build.Inputs, inp *Input) {
	if inp == nil || inp.State == nil {
		return
	}
	if t.ContextPath == "." {
		t.ContextPath = inp.URL
		return
	}
	st := llb.Scratch().File(llb.Copy(*inp.State, t.ContextPath, "/"), llb.WithCustomNamef("set context to %s", t.ContextPath))
	t.ContextState = &st
}

func toBuildOpt(t *Target, inp *Input) (*build.Options, error) {
	if v := t.Context; v != nil && *v == "-" {
		return nil, errors.Errorf("context from stdin not allowed in bake")
	}
	if v := t.Dockerfile; v != nil && *v == "-" {
		return nil, errors.Errorf("dockerfile from stdin not allowed in bake")
	}

	contextPath := "."
	if t.Context != nil {
		contextPath = *t.Context
	}
	contextPath = path.Clean(contextPath)
	dockerfilePath := "Dockerfile"
	if t.Dockerfile != nil {
		dockerfilePath = *t.Dockerfile
	}

	if !isRemoteResource(contextPath) && !path.IsAbs(dockerfilePath) {
		dockerfilePath = path.Join(contextPath, dockerfilePath)
	}

	noCache := false
	if t.NoCache != nil {
		noCache = *t.NoCache
	}
	pull := false
	if t.Pull != nil {
		pull = *t.Pull
	}

	bi := build.Inputs{
		ContextPath:    contextPath,
		DockerfilePath: dockerfilePath,
	}
	updateContext(&bi, inp)

	bo := &build.Options{
		Inputs:    bi,
		Tags:      t.Tags,
		BuildArgs: t.Args,
		Labels:    t.Labels,
		NoCache:   noCache,
		Pull:      pull,
	}

	platforms, err := platformutil.Parse(t.Platforms)
	if err != nil {
		return nil, err
	}
	bo.Platforms = platforms

	bo.Session = append(bo.Session, authprovider.NewDockerAuthProvider(os.Stderr))

	secrets, err := build.ParseSecretSpecs(t.Secrets)
	if err != nil {
		return nil, err
	}
	bo.Session = append(bo.Session, secrets)

	ssh, err := build.ParseSSHSpecs(t.SSH)
	if err != nil {
		return nil, err
	}
	bo.Session = append(bo.Session, ssh)

	if t.Target != nil {
		bo.Target = *t.Target
	}

	cacheImports, err := build.ParseCacheEntry(t.CacheFrom)
	if err != nil {
		return nil, err
	}
	bo.CacheFrom = cacheImports

	cacheExports, err := build.ParseCacheEntry(t.CacheTo)
	if err != nil {
		return nil, err
	}
	bo.CacheTo = cacheExports

	outputs, err := build.ParseOutputs(t.Outputs)
	if err != nil {
		return nil, err
	}
	bo.Exports = outputs

	return bo, nil
}

func defaultTarget() *Target {
	return &Target{}
}

func merge(t1, t2 *Target) *Target {
	if t2.Context != nil {
		t1.Context = t2.Context
	}
	if t2.Dockerfile != nil {
		t1.Dockerfile = t2.Dockerfile
	}
	for k, v := range t2.Args {
		if t1.Args == nil {
			t1.Args = map[string]string{}
		}
		t1.Args[k] = v
	}
	for k, v := range t2.Labels {
		if t1.Labels == nil {
			t1.Labels = map[string]string{}
		}
		t1.Labels[k] = v
	}
	if t2.Tags != nil { // no merge
		t1.Tags = t2.Tags
	}
	if t2.Target != nil {
		t1.Target = t2.Target
	}
	if t2.Secrets != nil { // merge
		t1.Secrets = append(t1.Secrets, t2.Secrets...)
	}
	if t2.SSH != nil { // merge
		t1.SSH = append(t1.SSH, t2.SSH...)
	}
	if t2.Platforms != nil { // no merge
		t1.Platforms = t2.Platforms
	}
	if t2.CacheFrom != nil { // no merge
		t1.CacheFrom = append(t1.CacheFrom, t2.CacheFrom...)
	}
	if t2.CacheTo != nil { // no merge
		t1.CacheTo = t2.CacheTo
	}
	if t2.Outputs != nil { // no merge
		t1.Outputs = t2.Outputs
	}
	if t2.Pull != nil {
		t1.Pull = t2.Pull
	}
	if t2.NoCache != nil {
		t1.NoCache = t2.NoCache
	}
	t1.Inherits = append(t1.Inherits, t2.Inherits...)
	return t1
}

func removeDupes(s []string) []string {
	i := 0
	seen := make(map[string]struct{}, len(s))
	for _, v := range s {
		if _, ok := seen[v]; ok {
			continue
		}
		if v == "" {
			continue
		}
		seen[v] = struct{}{}
		s[i] = v
		i++
	}
	return s[:i]
}

func isRemoteResource(str string) bool {
	return urlutil.IsGitURL(str) || urlutil.IsURL(str)
}
