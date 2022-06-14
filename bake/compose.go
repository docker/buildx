package bake

import (
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/compose-spec/compose-go/loader"
	compose "github.com/compose-spec/compose-go/types"
	"github.com/pkg/errors"
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

// composeExtTarget converts Compose build extension x-bake to bake Target
// https://github.com/compose-spec/compose-spec/blob/master/spec.md#extension
func (t *Target) composeExtTarget(exts map[string]interface{}) error {
	if ext, ok := exts["x-bake"]; ok {
		for key, val := range ext.(map[string]interface{}) {
			switch key {
			case "tags":
				if res, k := val.(string); k {
					t.Tags = append(t.Tags, res)
				} else {
					for _, res := range val.([]interface{}) {
						t.Tags = append(t.Tags, res.(string))
					}
				}
			case "cache-from":
				t.CacheFrom = []string{} // Needed to override the main field
				if res, k := val.(string); k {
					t.CacheFrom = append(t.CacheFrom, res)
				} else {
					for _, res := range val.([]interface{}) {
						t.CacheFrom = append(t.CacheFrom, res.(string))
					}
				}
			case "cache-to":
				if res, k := val.(string); k {
					t.CacheTo = append(t.CacheTo, res)
				} else {
					for _, res := range val.([]interface{}) {
						t.CacheTo = append(t.CacheTo, res.(string))
					}
				}
			case "secret":
				if res, k := val.(string); k {
					t.Secrets = append(t.Secrets, res)
				} else {
					for _, res := range val.([]interface{}) {
						t.Secrets = append(t.Secrets, res.(string))
					}
				}
			case "ssh":
				if res, k := val.(string); k {
					t.SSH = append(t.SSH, res)
				} else {
					for _, res := range val.([]interface{}) {
						t.SSH = append(t.SSH, res.(string))
					}
				}
			case "platforms":
				if res, k := val.(string); k {
					t.Platforms = append(t.Platforms, res)
				} else {
					for _, res := range val.([]interface{}) {
						t.Platforms = append(t.Platforms, res.(string))
					}
				}
			case "output":
				if res, k := val.(string); k {
					t.Outputs = append(t.Outputs, res)
				} else {
					for _, res := range val.([]interface{}) {
						t.Outputs = append(t.Outputs, res.(string))
					}
				}
			case "pull":
				if res, ok := val.(bool); ok {
					t.Pull = &res
				}
			case "no-cache":
				if res, ok := val.(bool); ok {
					t.NoCache = &res
				}
			case "no-cache-filter":
				if res, k := val.(string); k {
					t.NoCacheFilter = append(t.NoCacheFilter, res)
				} else {
					for _, res := range val.([]interface{}) {
						t.NoCacheFilter = append(t.NoCacheFilter, res.(string))
					}
				}
			default:
				return fmt.Errorf("compose file invalid: unkwown %s field for x-bake", key)
			}
		}
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
