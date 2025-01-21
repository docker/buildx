package buildflags

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zclconf/go-cty/cty"
)

func TestSecrets(t *testing.T) {
	t.Run("MarshalJSON", func(t *testing.T) {
		secrets := Secrets{
			{ID: "mysecret", FilePath: "/local/secret"},
			{ID: "mysecret2", Env: "TOKEN"},
		}

		expected := `[{"id":"mysecret","src":"/local/secret"},{"id":"mysecret2","env":"TOKEN"}]`
		actual, err := json.Marshal(secrets)
		require.NoError(t, err)
		require.JSONEq(t, expected, string(actual))
	})

	t.Run("UnmarshalJSON", func(t *testing.T) {
		in := `[{"id":"mysecret","src":"/local/secret"},{"id":"mysecret2","env":"TOKEN"}]`

		var actual Secrets
		err := json.Unmarshal([]byte(in), &actual)
		require.NoError(t, err)

		expected := Secrets{
			{ID: "mysecret", FilePath: "/local/secret"},
			{ID: "mysecret2", Env: "TOKEN"},
		}
		require.Equal(t, expected, actual)
	})

	t.Run("FromCtyValue", func(t *testing.T) {
		in := cty.TupleVal([]cty.Value{
			cty.ObjectVal(map[string]cty.Value{
				"id":  cty.StringVal("mysecret"),
				"src": cty.StringVal("/local/secret"),
			}),
			cty.ObjectVal(map[string]cty.Value{
				"id":  cty.StringVal("mysecret2"),
				"env": cty.StringVal("TOKEN"),
			}),
		})

		var actual Secrets
		err := actual.FromCtyValue(in, nil)
		require.NoError(t, err)

		expected := Secrets{
			{ID: "mysecret", FilePath: "/local/secret"},
			{ID: "mysecret2", Env: "TOKEN"},
		}
		require.Equal(t, expected, actual)
	})

	t.Run("ToCtyValue", func(t *testing.T) {
		secrets := Secrets{
			{ID: "mysecret", FilePath: "/local/secret"},
			{ID: "mysecret2", Env: "TOKEN"},
		}

		actual := secrets.ToCtyValue()
		expected := cty.ListVal([]cty.Value{
			cty.ObjectVal(map[string]cty.Value{
				"id":  cty.StringVal("mysecret"),
				"src": cty.StringVal("/local/secret"),
				"env": cty.StringVal(""),
			}),
			cty.ObjectVal(map[string]cty.Value{
				"id":  cty.StringVal("mysecret2"),
				"src": cty.StringVal(""),
				"env": cty.StringVal("TOKEN"),
			}),
		})

		result := actual.Equals(expected)
		require.True(t, result.True())
	})
}
