package bake

import (
	"fmt"
	"os"
	"strings"

	"github.com/compose-spec/compose-go/loader"
	compose "github.com/compose-spec/compose-go/types"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v3"
)

// errComposeInvalid is returned when a compose file is invalid
var errComposeInvalid = errors.New("invalid compose file")

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
		options.SkipConsistencyCheck = true
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
	if err = composeValidate(cfg); err != nil {
		return nil, err
	}

	var c Config
	if len(cfg.Services) > 0 {
		c.Groups = []*Group{}
		c.Targets = []*Target{}

		g := &Group{Name: "default"}

		for _, s := range cfg.Services {
			if s.Build == nil {
				s.Build = &compose.BuildConfig{}
			}

			targetName := sanitizeTargetName(s.Name)
			if err = validateTargetName(targetName); err != nil {
				return nil, errors.Wrapf(err, "invalid service name %q", targetName)
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

			g.Targets = append(g.Targets, targetName)
			t := &Target{
				Name:       targetName,
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
				CacheTo:     s.Build.CacheTo,
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
	Contexts      stringMap   `yaml:"contexts,omitempty"`
	// don't forget to update documentation if you add a new field:
	// docs/guides/bake/compose-file.md#extension-field-with-x-bake
}

type stringMap map[string]string
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
		t.Tags = dedupSlice(append(t.Tags, xb.Tags...))
	}
	if len(xb.CacheFrom) > 0 {
		t.CacheFrom = dedupSlice(append(t.CacheFrom, xb.CacheFrom...))
	}
	if len(xb.CacheTo) > 0 {
		t.CacheTo = dedupSlice(append(t.CacheTo, xb.CacheTo...))
	}
	if len(xb.Secrets) > 0 {
		t.Secrets = dedupSlice(append(t.Secrets, xb.Secrets...))
	}
	if len(xb.SSH) > 0 {
		t.SSH = dedupSlice(append(t.SSH, xb.SSH...))
	}
	if len(xb.Platforms) > 0 {
		t.Platforms = dedupSlice(append(t.Platforms, xb.Platforms...))
	}
	if len(xb.Outputs) > 0 {
		t.Outputs = dedupSlice(append(t.Outputs, xb.Outputs...))
	}
	if xb.Pull != nil {
		t.Pull = xb.Pull
	}
	if xb.NoCache != nil {
		t.NoCache = xb.NoCache
	}
	if len(xb.NoCacheFilter) > 0 {
		t.NoCacheFilter = dedupSlice(append(t.NoCacheFilter, xb.NoCacheFilter...))
	}
	if len(xb.Contexts) > 0 {
		t.Contexts = dedupMap(t.Contexts, xb.Contexts)
	}

	return nil
}

// composeValidate validates a compose file
func composeValidate(project *compose.Project) error {
	for _, s := range project.Services {
		if s.Build != nil {
			for _, secret := range s.Build.Secrets {
				if _, ok := project.Secrets[secret.Source]; !ok {
					return errors.Wrap(errComposeInvalid, fmt.Sprintf("service %q refers to undefined build secret %s", sanitizeTargetName(s.Name), secret.Source))
				}
			}
		}
	}
	for name, secret := range project.Secrets {
		if secret.External.External {
			continue
		}
		if secret.File == "" && secret.Environment == "" {
			return errors.Wrap(errComposeInvalid, fmt.Sprintf("secret %q must declare either `file` or `environment`", name))
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
