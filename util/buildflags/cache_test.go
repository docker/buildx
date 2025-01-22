package buildflags

import (
	"encoding/json"
	"testing"

	"github.com/docker/buildx/controller/pb"
	"github.com/stretchr/testify/require"
	"github.com/zclconf/go-cty/cty"
)

func TestCacheOptions_DerivedVars(t *testing.T) {
	t.Setenv("ACTIONS_RUNTIME_TOKEN", "sensitive_token")
	t.Setenv("ACTIONS_CACHE_URL", "https://cache.github.com")
	t.Setenv("AWS_ACCESS_KEY_ID", "definitely_dont_look_here")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "hackers_please_dont_steal")
	t.Setenv("AWS_SESSION_TOKEN", "not_a_mitm_attack")

	cacheFrom, err := ParseCacheEntry([]string{"type=gha", "type=s3,region=us-west-2,bucket=my_bucket,name=my_image"})
	require.NoError(t, err)
	require.Equal(t, []*pb.CacheOptionsEntry{
		{
			Type: "gha",
			Attrs: map[string]string{
				"token": "sensitive_token",
				"url":   "https://cache.github.com",
			},
		},
		{
			Type: "s3",
			Attrs: map[string]string{
				"region":            "us-west-2",
				"bucket":            "my_bucket",
				"name":              "my_image",
				"access_key_id":     "definitely_dont_look_here",
				"secret_access_key": "hackers_please_dont_steal",
				"session_token":     "not_a_mitm_attack",
			},
		},
	}, cacheFrom)
}

func TestCacheOptions(t *testing.T) {
	t.Run("MarshalJSON", func(t *testing.T) {
		cache := CacheOptions{
			{Type: "registry", Attrs: map[string]string{"ref": "user/app:cache"}},
			{Type: "local", Attrs: map[string]string{"src": "path/to/cache"}},
		}

		expected := `[{"type":"registry","ref":"user/app:cache"},{"type":"local","src":"path/to/cache"}]`
		actual, err := json.Marshal(cache)
		require.NoError(t, err)
		require.JSONEq(t, expected, string(actual))
	})

	t.Run("UnmarshalJSON", func(t *testing.T) {
		in := `[{"type":"registry","ref":"user/app:cache"},{"type":"local","src":"path/to/cache"}]`

		var actual CacheOptions
		err := json.Unmarshal([]byte(in), &actual)
		require.NoError(t, err)

		expected := CacheOptions{
			{Type: "registry", Attrs: map[string]string{"ref": "user/app:cache"}},
			{Type: "local", Attrs: map[string]string{"src": "path/to/cache"}},
		}
		require.Equal(t, expected, actual)
	})

	t.Run("FromCtyValue", func(t *testing.T) {
		in := cty.TupleVal([]cty.Value{
			cty.ObjectVal(map[string]cty.Value{
				"type": cty.StringVal("registry"),
				"ref":  cty.StringVal("user/app:cache"),
			}),
			cty.StringVal("type=local,src=path/to/cache"),
		})

		var actual CacheOptions
		err := actual.FromCtyValue(in, nil)
		require.NoError(t, err)

		expected := CacheOptions{
			{Type: "registry", Attrs: map[string]string{"ref": "user/app:cache"}},
			{Type: "local", Attrs: map[string]string{"src": "path/to/cache"}},
		}
		require.Equal(t, expected, actual)
	})

	t.Run("ToCtyValue", func(t *testing.T) {
		attests := CacheOptions{
			{Type: "registry", Attrs: map[string]string{"ref": "user/app:cache"}},
			{Type: "local", Attrs: map[string]string{"src": "path/to/cache"}},
		}

		actual := attests.ToCtyValue()
		expected := cty.ListVal([]cty.Value{
			cty.MapVal(map[string]cty.Value{
				"type": cty.StringVal("registry"),
				"ref":  cty.StringVal("user/app:cache"),
			}),
			cty.MapVal(map[string]cty.Value{
				"type": cty.StringVal("local"),
				"src":  cty.StringVal("path/to/cache"),
			}),
		})

		result := actual.Equals(expected)
		require.True(t, result.True())
	})
}
