package bake

import (
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/docker/cli/cli/compose/loader"
	composetypes "github.com/docker/cli/cli/compose/types"
)

func parseCompose(dt []byte) (*composetypes.Config, error) {
	parsed, err := loader.ParseYAML([]byte(dt))
	if err != nil {
		return nil, err
	}
	return loader.Load(composetypes.ConfigDetails{
		ConfigFiles: []composetypes.ConfigFile{
			{
				Config: parsed,
			},
		},
		Environment: envMap(os.Environ()),
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
	var zeroBuildConfig composetypes.BuildConfig
	if len(cfg.Services) > 0 {
		c.Groups = []*Group{}
		c.Targets = []*Target{}

		g := &Group{Name: "default"}

		for _, s := range cfg.Services {

			if reflect.DeepEqual(s.Build, zeroBuildConfig) {
				// if not make sure they're setting an image or it's invalid d-c.yml
				if s.Image == "" {
					return nil, fmt.Errorf("compose file invalid: service %s has neither an image nor a build context specified. At least one must be provided", s.Name)
				}
				continue
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
			g.Targets = append(g.Targets, s.Name)
			t := &Target{
				Name:       s.Name,
				Context:    contextPathP,
				Dockerfile: dockerfilePathP,
				Labels:     s.Build.Labels,
				Args:       toMap(s.Build.Args),
				CacheFrom:  s.Build.CacheFrom,
				// TODO: add platforms
			}
			if s.Build.Target != "" {
				target := s.Build.Target
				t.Target = &target
			}
			if s.Image != "" {
				t.Tags = []string{s.Image}
			}
			c.Targets = append(c.Targets, t)
		}
		c.Groups = append(c.Groups, g)

	}

	return &c, nil
}

func toMap(in composetypes.MappingWithEquals) map[string]string {
	m := map[string]string{}
	for k, v := range in {
		if v != nil {
			m[k] = *v
		} else {
			m[k] = os.Getenv(k)
		}
	}
	return m
}
