package buildflags

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zclconf/go-cty/cty"
)

func TestAttests(t *testing.T) {
	t.Run("MarshalJSON", func(t *testing.T) {
		attests := Attests{
			{Type: "provenance", Attrs: map[string]string{"mode": "max"}},
			{Type: "sbom", Disabled: true},
		}

		expected := `[{"type":"provenance","mode":"max"},{"type":"sbom","disabled":true}]`
		actual, err := json.Marshal(attests)
		require.NoError(t, err)
		require.JSONEq(t, expected, string(actual))
	})

	t.Run("UnmarshalJSON", func(t *testing.T) {
		in := `[{"type":"provenance","mode":"max"},{"type":"sbom","disabled":true}]`

		var actual Attests
		err := json.Unmarshal([]byte(in), &actual)
		require.NoError(t, err)

		expected := Attests{
			{Type: "provenance", Attrs: map[string]string{"mode": "max"}},
			{Type: "sbom", Disabled: true, Attrs: map[string]string{}},
		}
		require.Equal(t, expected, actual)
	})

	t.Run("FromCtyValue", func(t *testing.T) {
		in := cty.TupleVal([]cty.Value{
			cty.ObjectVal(map[string]cty.Value{
				"type": cty.StringVal("provenance"),
				"mode": cty.StringVal("max"),
			}),
			cty.StringVal("type=sbom,disabled=true"),
		})

		var actual Attests
		err := actual.FromCtyValue(in, nil)
		require.NoError(t, err)

		expected := Attests{
			{Type: "provenance", Attrs: map[string]string{"mode": "max"}},
			{Type: "sbom", Disabled: true, Attrs: map[string]string{}},
		}
		require.Equal(t, expected, actual)
	})

	t.Run("ToCtyValue", func(t *testing.T) {
		attests := Attests{
			{Type: "provenance", Attrs: map[string]string{"mode": "max"}},
			{Type: "sbom", Disabled: true},
		}

		actual := attests.ToCtyValue()
		expected := cty.ListVal([]cty.Value{
			cty.MapVal(map[string]cty.Value{
				"type": cty.StringVal("provenance"),
				"mode": cty.StringVal("max"),
			}),
			cty.MapVal(map[string]cty.Value{
				"type":     cty.StringVal("sbom"),
				"disabled": cty.StringVal("true"),
			}),
		})

		result := actual.Equals(expected)
		require.True(t, result.True())
	})
}
