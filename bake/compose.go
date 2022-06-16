package bake

import (
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/compose-spec/compose-go/loader"
	compose "github.com/compose-spec/compose-go/types"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v3"
)

func parseCompose(dt []byte) (*compose.Project, error) {
	return loader.Load(compose.ConfigDetails{
		ConfigFiles: []compose.ConfigFile{
			{
				Content: dt,
			},
		},
		Environment: envMap(os.Environ()),
	}, func(options *loader.Options) {
		options.SkipNormalization = true
	})
}

func envMap(env []string) map[string]string {
	result := make(map[string]string, len(env))
	for _, s := range env {
		kv := strings.SplitN(s, "=", 2)
		if len(kv) != 2 {
			continue
		}
		result[kv[0]] = kv[1]
	}
	return result
}

func ParseCompose(dt []byte) (*Config, error) {
	cfg, err := parseCompose(dt)
	if err != nil {
		return nil, err
	}

	var c Config
	var zeroBuildConfig compose.BuildConfig
	if len(cfg.Services) > 0 {
		c.Groups = []*Group{}
		c.Targets = []*Target{}

		g := &Group{Name: "default"}

		for _, s := range cfg.Services {

			if s.Build == nil || reflect.DeepEqual(s.Build, zeroBuildConfig) {
				// if not make sure they're setting an image or it's invalid d-c.yml
				if s.Image == "" {
					return nil, fmt.Errorf("compose file invalid: service %s has neither an image nor a build context specified. At least one must be provided", s.Name)
				}
				continue
			}

			if err = validateTargetName(s.Name); err != nil {
				return nil, errors.Wrapf(err, "invalid service name %q", s.Name)
			}

			var contextPathP *string
			if s.Build.Context != "" {
				contextPath := s.Build.Context
				contextPathP = &contextPath
			}
			var dockerfilePathP *string
			if s.Build.Dockerfile != "" {
				dockerfilePath := s.Build.Dockerfile
				dockerfilePathP = &dockerfilePath
			}

			var secrets []string
			for _, bs := range s.Build.Secrets {
				secret, err := composeToBuildkitSecret(bs, cfg.Secrets[bs.Source])
				if err != nil {
					return nil, err
				}
				secrets = append(secrets, secret)
			}

			g.Targets = append(g.Targets, s.Name)
			t := &Target{
				Name:       s.Name,
				Context:    contextPathP,
				Dockerfile: dockerfilePathP,
				Tags:       s.Build.Tags,
				Labels:     s.Build.Labels,
				Args: flatten(s.Build.Args.Resolve(func(val string) (string, bool) {
					if val, ok := s.Environment[val]; ok && val != nil {
						return *val, true
					}
					val, ok := cfg.Environment[val]
					return val, ok
				})),
				CacheFrom:   s.Build.CacheFrom,
				NetworkMode: &s.Build.Network,
				Secrets:     secrets,
			}
			if err = t.composeExtTarget(s.Build.Extensions); err != nil {
				return nil, err
			}
			if s.Build.Target != "" {
				target := s.Build.Target
				t.Target = &target
			}
			if len(t.Tags) == 0 && s.Image != "" {
				t.Tags = []string{s.Image}
			}
			c.Targets = append(c.Targets, t)
		}
		c.Groups = append(c.Groups, g)

	}

	return &c, nil
}

func flatten(in compose.MappingWithEquals) compose.Mapping {
	if len(in) == 0 {
		return nil
	}
	out := compose.Mapping{}
	for k, v := range in {
		if v == nil {
			continue
		}
		out[k] = *v
	}
	return out
}

// xbake Compose build extension provides fields not (yet) available in
// Compose build specification: https://github.com/compose-spec/compose-spec/blob/master/build.md
type xbake struct {
	Tags          stringArray `yaml:"tags,omitempty"`
	CacheFrom     stringArray `yaml:"cache-from,omitempty"`
	CacheTo       stringArray `yaml:"cache-to,omitempty"`
	Secrets       stringArray `yaml:"secret,omitempty"`
	SSH           stringArray `yaml:"ssh,omitempty"`
	Platforms     stringArray `yaml:"platforms,omitempty"`
	Outputs       stringArray `yaml:"output,omitempty"`
	Pull          *bool       `yaml:"pull,omitempty"`
	NoCache       *bool       `yaml:"no-cache,omitempty"`
	NoCacheFilter stringArray `yaml:"no-cache-filter,omitempty"`
	// don't forget to update documentation if you add a new field:
	// docs/guides/bake/compose-file.md#extension-field-with-x-bake
}

type stringArray []string

func (sa *stringArray) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var multi []string
	err := unmarshal(&multi)
	if err != nil {
		var single string
		if err := unmarshal(&single); err != nil {
			return err
		}
		*sa = strings.Fields(single)
	} else {
		*sa = multi
	}
	return nil
}

// composeExtTarget converts Compose build extension x-bake to bake Target
// https://github.com/compose-spec/compose-spec/blob/master/spec.md#extension
func (t *Target) composeExtTarget(exts map[string]interface{}) error {
	var xb xbake

	ext, ok := exts["x-bake"]
	if !ok || ext == nil {
		return nil
	}

	yb, _ := yaml.Marshal(ext)
	if err := yaml.Unmarshal(yb, &xb); err != nil {
		return err
	}

	if len(xb.Tags) > 0 {
		t.Tags = append(t.Tags, xb.Tags...)
	}
	if len(xb.CacheFrom) > 0 {
		t.CacheFrom = xb.CacheFrom // override main field
	}
	if len(xb.CacheTo) > 0 {
		t.CacheTo = append(t.CacheTo, xb.CacheTo...)
	}
	if len(xb.Secrets) > 0 {
		t.Secrets = append(t.Secrets, xb.Secrets...)
	}
	if len(xb.SSH) > 0 {
		t.SSH = append(t.SSH, xb.SSH...)
	}
	if len(xb.Platforms) > 0 {
		t.Platforms = append(t.Platforms, xb.Platforms...)
	}
	if len(xb.Outputs) > 0 {
		t.Outputs = append(t.Outputs, xb.Outputs...)
	}
	if xb.Pull != nil {
		t.Pull = xb.Pull
	}
	if xb.NoCache != nil {
		t.NoCache = xb.NoCache
	}
	if len(xb.NoCacheFilter) > 0 {
		t.NoCacheFilter = append(t.NoCacheFilter, xb.NoCacheFilter...)
	}

	return nil
}

// composeToBuildkitSecret converts secret from compose format to buildkit's
// csv format.
func composeToBuildkitSecret(inp compose.ServiceSecretConfig, psecret compose.SecretConfig) (string, error) {
	if psecret.External.External {
		return "", errors.Errorf("unsupported external secret %s", psecret.Name)
	}

	var bkattrs []string
	if inp.Source != "" {
		bkattrs = append(bkattrs, "id="+inp.Source)
	}
	if psecret.File != "" {
		bkattrs = append(bkattrs, "src="+psecret.File)
	}
	if psecret.Environment != "" {
		bkattrs = append(bkattrs, "env="+psecret.Environment)
	}

	return strings.Join(bkattrs, ","), nil
}
