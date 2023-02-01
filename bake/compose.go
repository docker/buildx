package bake

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/compose-spec/compose-go/dotenv"
	"github.com/compose-spec/compose-go/loader"
	compose "github.com/compose-spec/compose-go/types"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v3"
)

func ParseComposeFiles(fs []File) (*Config, error) {
	envs, err := composeEnv()
	if err != nil {
		return nil, err
	}
	var cfgs []compose.ConfigFile
	for _, f := range fs {
		cfgs = append(cfgs, compose.ConfigFile{
			Filename: f.Name,
			Content:  f.Data,
		})
	}
	return ParseCompose(cfgs, envs)
}

func ParseCompose(cfgs []compose.ConfigFile, envs map[string]string) (*Config, error) {
	cfg, err := loader.Load(compose.ConfigDetails{
		ConfigFiles: cfgs,
		Environment: envs,
	}, func(options *loader.Options) {
		options.SkipNormalization = true
	})
	if err != nil {
		return nil, err
	}

	var c Config
	if len(cfg.Services) > 0 {
		c.Groups = []*Group{}
		c.Targets = []*Target{}

		g := &Group{Name: "default"}

		for _, s := range cfg.Services {
			if s.Build == nil {
				continue
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

			// compose does not support nil values for labels
			labels := map[string]*string{}
			for k, v := range s.Build.Labels {
				v := v
				labels[k] = &v
			}

			g.Targets = append(g.Targets, targetName)
			t := &Target{
				Name:       targetName,
				Context:    contextPathP,
				Dockerfile: dockerfilePathP,
				Tags:       s.Build.Tags,
				Labels:     labels,
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

func validateComposeFile(dt []byte, fn string) (bool, error) {
	envs, err := composeEnv()
	if err != nil {
		return true, err
	}
	fnl := strings.ToLower(fn)
	if strings.HasSuffix(fnl, ".yml") || strings.HasSuffix(fnl, ".yaml") {
		return true, validateCompose(dt, envs)
	}
	if strings.HasSuffix(fnl, ".json") || strings.HasSuffix(fnl, ".hcl") {
		return false, nil
	}
	err = validateCompose(dt, envs)
	return err == nil, err
}

func validateCompose(dt []byte, envs map[string]string) error {
	_, err := loader.Load(compose.ConfigDetails{
		ConfigFiles: []compose.ConfigFile{
			{
				Content: dt,
			},
		},
		Environment: envs,
	}, func(options *loader.Options) {
		options.SkipNormalization = true
		// consistency is checked later in ParseCompose to ensure multiple
		// compose files can be merged together
		options.SkipConsistencyCheck = true
	})
	return err
}

func composeEnv() (map[string]string, error) {
	envs := sliceToMap(os.Environ())
	if wd, err := os.Getwd(); err == nil {
		envs, err = loadDotEnv(envs, wd)
		if err != nil {
			return nil, err
		}
	}
	return envs, nil
}

func loadDotEnv(curenv map[string]string, workingDir string) (map[string]string, error) {
	if curenv == nil {
		curenv = make(map[string]string)
	}

	ef, err := filepath.Abs(filepath.Join(workingDir, ".env"))
	if err != nil {
		return nil, err
	}

	if _, err = os.Stat(ef); os.IsNotExist(err) {
		return curenv, nil
	} else if err != nil {
		return nil, err
	}

	dt, err := os.ReadFile(ef)
	if err != nil {
		return nil, err
	}

	envs, err := dotenv.UnmarshalBytes(dt)
	if err != nil {
		return nil, err
	}

	for k, v := range envs {
		if _, set := curenv[k]; set {
			continue
		}
		curenv[k] = v
	}

	return curenv, nil
}

func flatten(in compose.MappingWithEquals) map[string]*string {
	if len(in) == 0 {
		return nil
	}
	out := map[string]*string{}
	for k, v := range in {
		if v == nil {
			continue
		}
		out[k] = v
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
	// docs/manuals/bake/compose-file.md#extension-field-with-x-bake
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
