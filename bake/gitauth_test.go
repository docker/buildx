package bake

import (
	"testing"

	"github.com/docker/buildx/util/buildflags"
	"github.com/moby/buildkit/client/llb"
	"github.com/stretchr/testify/require"
)

func TestGitAuthSecretsFromEnviron(t *testing.T) {
	t.Run("base keys", func(t *testing.T) {
		secrets := gitAuthSecretsFromEnviron([]string{
			bakeGitAuthTokenEnv + "=token",
			bakeGitAuthHeaderEnv + "=basic",
		})
		require.Equal(t, []string{
			llb.GitAuthTokenKey + "|" + bakeGitAuthTokenEnv,
			llb.GitAuthHeaderKey + "|" + bakeGitAuthHeaderEnv,
		}, secretPairs(secrets))
	})
	t.Run("domain suffix keys", func(t *testing.T) {
		secrets := gitAuthSecretsFromEnviron([]string{
			bakeGitAuthTokenEnv + ".github.com=token",
			bakeGitAuthHeaderEnv + ".github.com=bearer",
			bakeGitAuthTokenEnv + ".example.com=token2",
			bakeGitAuthTokenEnv + "=fallback",
		})
		require.Equal(t, []string{
			llb.GitAuthTokenKey + "|" + bakeGitAuthTokenEnv,
			llb.GitAuthTokenKey + ".example.com|" + bakeGitAuthTokenEnv + ".example.com",
			llb.GitAuthTokenKey + ".github.com|" + bakeGitAuthTokenEnv + ".github.com",
			llb.GitAuthHeaderKey + ".github.com|" + bakeGitAuthHeaderEnv + ".github.com",
		}, secretPairs(secrets))
	})
	t.Run("ignores non-domain suffix", func(t *testing.T) {
		secrets := gitAuthSecretsFromEnviron([]string{
			bakeGitAuthTokenEnv + "_EXTRA=token",
			bakeGitAuthHeaderEnv + "-extra=basic",
			bakeGitAuthTokenEnv + ".=bad",
			bakeGitAuthHeaderEnv + ".=bad",
		})
		require.Empty(t, secrets)
	})
}

func secretPairs(secrets buildflags.Secrets) []string {
	out := make([]string, 0, len(secrets))
	for _, s := range secrets {
		out = append(out, s.ID+"|"+s.Env)
	}
	return out
}
