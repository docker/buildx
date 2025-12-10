package dockerconfig

import (
	"cmp"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/docker/buildx/util/confutil"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/config"
	"github.com/docker/cli/cli/config/configfile"
	"github.com/docker/cli/cli/config/types"
	"github.com/moby/buildkit/session/auth/authprovider"
)

func LoadAuthConfig(cli command.Cli) authprovider.AuthConfigProvider {
	acp := &authConfigProvider{
		buildxConfig:    confutil.NewConfig(cli),
		defaultConfig:   cli.ConfigFile(),
		authConfigCache: map[string]authConfigCacheEntry{},
	}
	return acp.load
}

type authConfigProvider struct {
	initOnce           sync.Once
	defaultConfig      *configfile.ConfigFile
	buildxConfig       *confutil.Config
	authConfigCache    map[string]authConfigCacheEntry
	mu                 sync.Mutex // mutex for authConfigCache
	alternativeConfigs []*alternativeConfig
}

func (ap *authConfigProvider) load(ctx context.Context, host string, scopes []string, cacheExpireCheck authprovider.ExpireCachedAuthCheck) (types.AuthConfig, error) {
	ap.initOnce.Do(func() {
		ap.init()
	})

	ap.mu.Lock()
	defer ap.mu.Unlock()

	candidates := []*alternativeConfig{}
	parsedScopes := parseScopes(scopes)

	if len(parsedScopes) == 1 {
		for _, cfg := range ap.alternativeConfigs {
			if cfg.host != host {
				continue
			}
			if cfg.matchesScopes(parsedScopes) {
				candidates = append(candidates, cfg)
			}
		}
	}
	key := host
	cfg := ap.defaultConfig
	if len(candidates) > 0 {
		// matches with repo before those without repo
		// matches with scope set sorted before those without scope
		slices.SortFunc(candidates, func(a, b *alternativeConfig) int {
			return cmp.Or(
				strings.Compare(b.repo, a.repo),
				cmp.Compare(len(b.scope), len(a.scope)),
			)
		})
		candidates = candidates[:1]
		key += "|" + candidates[0].dir
		if candidates[0].configFile == nil {
			if cfgDir, err := config.Load(candidates[0].dir); err == nil {
				cfg = cfgDir
				candidates[0].configFile = cfg
			}
		} else {
			cfg = candidates[0].configFile
		}
	}

	entry, exists := ap.authConfigCache[key]
	if exists && (cacheExpireCheck == nil || !cacheExpireCheck(entry.Created, key)) {
		return *entry.Auth, nil
	}

	hostKey := host
	if host == authprovider.DockerHubRegistryHost {
		hostKey = authprovider.DockerHubConfigfileKey
	}

	ac, err := cfg.GetAuthConfig(hostKey)
	if err != nil {
		return types.AuthConfig{}, err
	}

	entry = authConfigCacheEntry{
		Created: time.Now(),
		Auth:    &ac,
	}

	ap.authConfigCache[key] = entry

	return ac, nil
}

func (ap *authConfigProvider) init() error {
	base := filepath.Join(ap.buildxConfig.Dir(), "config")
	return filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if d.Name() != config.ConfigFileName {
			return nil
		}
		dir := filepath.Dir(path)
		rdir, err := filepath.Rel(base, dir)
		if err != nil {
			return err
		}
		cfg := parseConfigKey(rdir)
		cfg.dir = dir
		ap.alternativeConfigs = append(ap.alternativeConfigs, &cfg)
		return nil
	})
}

func parseConfigKey(key string) alternativeConfig {
	var out alternativeConfig

	var mainPart, scopePart string
	if i := strings.IndexByte(key, '@'); i >= 0 {
		mainPart = key[:i]
		scopePart = key[i+1:]
	} else {
		mainPart = key
	}

	if scopePart != "" {
		out.scope = make(map[string]struct{})
		for s := range strings.SplitSeq(scopePart, ",") {
			if s = strings.TrimSpace(s); s != "" {
				out.scope[s] = struct{}{}
			}
		}
	}

	if mainPart == "" {
		return out
	}

	slash := strings.IndexByte(mainPart, '/')
	if slash < 0 {
		out.host = mainPart
		return out
	}

	out.host = mainPart[:slash]
	out.repo = mainPart[slash+1:]

	return out
}

type alternativeConfig struct {
	dir string

	host  string
	repo  string
	scope map[string]struct{}

	configFile *configfile.ConfigFile
}

func (a *alternativeConfig) matchesScopes(q scopes) bool {
	if a.repo != "" {
		if _, ok := q["repository:"+a.repo]; !ok {
			return false
		}
	}

	if len(a.scope) > 0 {
		if a.repo == "" {
			// no repo means one query must match all scopes
			for _, scopeActions := range q {
				ok := true
				for s := range a.scope {
					if _, exists := scopeActions[s]; !exists {
						ok = false
						break
					}
				}
				if ok {
					return true
				}
			}
			return false
		}
		for s := range a.scope {
			for k, scopeActions := range q {
				if k == "repository:"+a.repo {
					if _, ok := scopeActions[s]; !ok {
						return false
					}
				}
			}
		}
	}

	return true
}

type authConfigCacheEntry struct {
	Created time.Time
	Auth    *types.AuthConfig
}

type scopes map[string]map[string]struct{}

func parseScopes(s []string) scopes {
	// https://distribution.github.io/distribution/spec/auth/scope/
	m := map[string]map[string]struct{}{}
	for _, scopeStr := range s {
		if scopeStr == "" {
			return nil
		}
		// The scopeStr may have strings that contain multiple scopes separated by a space.
		for scope := range strings.SplitSeq(scopeStr, " ") {
			parts := strings.SplitN(scope, ":", 3)
			names := []string{parts[0]}
			if len(parts) > 1 {
				names = append(names, parts[1])
			}
			var actions []string
			if len(parts) == 3 {
				actions = append(actions, strings.Split(parts[2], ",")...)
			}
			name := strings.Join(names, ":")
			ma, ok := m[name]
			if !ok {
				ma = map[string]struct{}{}
				m[name] = ma
			}

			for _, a := range actions {
				ma[a] = struct{}{}
			}
		}
	}
	return m
}
