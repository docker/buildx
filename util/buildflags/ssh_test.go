package buildflags

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zclconf/go-cty/cty"
)

func TestSSHKeys(t *testing.T) {
	t.Run("MarshalJSON", func(t *testing.T) {
		sshkeys := SSHKeys{
			{ID: "default", Paths: []string{}},
			{ID: "key", Paths: []string{"path/to/key"}},
		}

		expected := `[{"id":"default"},{"id":"key","paths":["path/to/key"]}]`
		actual, err := json.Marshal(sshkeys)
		require.NoError(t, err)
		require.JSONEq(t, expected, string(actual))
	})

	t.Run("UnmarshalJSON", func(t *testing.T) {
		in := `[{"id":"default"},{"id":"key","paths":["path/to/key"]}]`

		var actual SSHKeys
		err := json.Unmarshal([]byte(in), &actual)
		require.NoError(t, err)

		expected := SSHKeys{
			{ID: "default"},
			{ID: "key", Paths: []string{"path/to/key"}},
		}
		require.Equal(t, expected, actual)
	})

	t.Run("FromCtyValue", func(t *testing.T) {
		in := cty.TupleVal([]cty.Value{
			cty.ObjectVal(map[string]cty.Value{
				"id": cty.StringVal("default"),
			}),
			cty.ObjectVal(map[string]cty.Value{
				"id": cty.StringVal("key"),
				"paths": cty.TupleVal([]cty.Value{
					cty.StringVal("path/to/key"),
				}),
			}),
		})

		var actual SSHKeys
		err := actual.FromCtyValue(in, nil)
		require.NoError(t, err)

		expected := SSHKeys{
			{ID: "default"},
			{ID: "key", Paths: []string{"path/to/key"}},
		}
		require.Equal(t, expected, actual)
	})

	t.Run("ToCtyValue", func(t *testing.T) {
		sshkeys := SSHKeys{
			{ID: "default", Paths: []string{}},
			{ID: "key", Paths: []string{"path/to/key"}},
		}

		actual := sshkeys.ToCtyValue()
		expected := cty.ListVal([]cty.Value{
			cty.ObjectVal(map[string]cty.Value{
				"id":    cty.StringVal("default"),
				"paths": cty.ListValEmpty(cty.String),
			}),
			cty.ObjectVal(map[string]cty.Value{
				"id": cty.StringVal("key"),
				"paths": cty.ListVal([]cty.Value{
					cty.StringVal("path/to/key"),
				}),
			}),
		})

		result := actual.Equals(expected)
		require.True(t, result.True())
	})
}
