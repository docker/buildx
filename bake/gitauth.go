package bake

import (
	"os"
	"sort"
	"strings"

	"github.com/docker/buildx/util/buildflags"
	"github.com/moby/buildkit/client/llb"
)

const (
	bakeGitAuthTokenEnv  = "BUILDX_BAKE_GIT_AUTH_TOKEN" // #nosec G101 -- environment variable key, not a credential
	bakeGitAuthHeaderEnv = "BUILDX_BAKE_GIT_AUTH_HEADER"
)

func gitAuthSecretsFromEnv() buildflags.Secrets {
	return gitAuthSecretsFromEnviron(os.Environ())
}

func gitAuthSecretsFromEnviron(environ []string) buildflags.Secrets {
	secrets := make(buildflags.Secrets, 0, 2)
	secrets = append(secrets, gitAuthSecretsForEnv(llb.GitAuthTokenKey, bakeGitAuthTokenEnv, environ)...)
	secrets = append(secrets, gitAuthSecretsForEnv(llb.GitAuthHeaderKey, bakeGitAuthHeaderEnv, environ)...)
	return secrets
}

func gitAuthSecretsForEnv(secretIDPrefix, envPrefix string, environ []string) buildflags.Secrets {
	envKeys := findGitAuthEnvKeys(envPrefix, environ)
	secrets := make(buildflags.Secrets, 0, len(envKeys))
	for _, envKey := range envKeys {
		suffix := envKey[len(envPrefix):]
		secrets = append(secrets, &buildflags.Secret{
			ID:  secretIDPrefix + suffix,
			Env: envKey,
		})
	}
	return secrets
}

func findGitAuthEnvKeys(envPrefix string, environ []string) []string {
	prefixUpper := strings.ToUpper(envPrefix)
	var keys []string
	for _, env := range environ {
		key, _, ok := strings.Cut(env, "=")
		if !ok {
			continue
		}
		if !strings.HasPrefix(strings.ToUpper(key), prefixUpper) {
			continue
		}
		if len(key) == len(envPrefix) {
			keys = append(keys, key)
			continue
		}
		if len(key) <= len(envPrefix)+1 {
			continue
		}
		if key[len(envPrefix)] == '.' {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}
