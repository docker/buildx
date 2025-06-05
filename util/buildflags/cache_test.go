package buildflags

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zclconf/go-cty/cty"
)

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

func TestCacheOptions_RefOnlyFormat(t *testing.T) {
	opts, err := ParseCacheEntry([]string{"ref1", "ref2"})
	require.NoError(t, err)
	require.Equal(t, CacheOptions{
		{Type: "registry", Attrs: map[string]string{"ref": "ref1"}},
		{Type: "registry", Attrs: map[string]string{"ref": "ref2"}},
	}, opts)
}
