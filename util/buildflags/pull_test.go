package buildflags

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zclconf/go-cty/cty"
	"go.yaml.in/yaml/v3"
)

func TestPullPolicy(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  PullPolicy
		json  string
	}{
		{
			input: "true",
			want:  "true",
			json:  "true",
		},
		{
			input: "false",
			want:  "false",
			json:  "false",
		},
		{
			input: "1",
			want:  "true",
			json:  "true",
		},
		{
			input: "FALSE",
			want:  "false",
			json:  "false",
		},
		{
			input: "always",
			want:  PullAlways,
			json:  `"always"`,
		},
		{
			input: "missing",
			want:  PullMissing,
			json:  `"missing"`,
		},
		{
			input: "never",
			want:  PullNever,
			json:  `"never"`,
		},
	} {
		t.Run(tc.input, func(t *testing.T) {
			var policy PullPolicy
			require.NoError(t, policy.Set(tc.input))
			require.Equal(t, tc.want, policy)
			dt, err := json.Marshal(policy)
			require.NoError(t, err)
			require.JSONEq(t, tc.json, string(dt))
			var decoded PullPolicy
			require.NoError(t, json.Unmarshal(dt, &decoded))
			require.Equal(t, policy, decoded)
			var fromCty PullPolicy
			require.NoError(t, fromCty.FromCtyValue(policy.ToCtyValue(), nil))
			require.Equal(t, policy, fromCty)
		})
	}
	for _, value := range []string{"", "auto", "--no-cache", "Always"} {
		var policy PullPolicy
		require.ErrorContains(t, policy.Set(value), "invalid pull policy")
	}
	var policy PullPolicy
	require.Error(t, policy.FromCtyValue(cty.NumberIntVal(1), nil))
	require.Error(t, json.Unmarshal([]byte("1"), &policy))
	policy = "true"
	require.NoError(t, json.Unmarshal([]byte("null"), &policy))
	require.Equal(t, PullPolicy("true"), policy)
}

func TestPullPolicyYAML(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  PullPolicy
	}{
		{
			input: "yes",
			want:  "true",
		},
		{
			input: "off",
			want:  "false",
		},
		{
			input: "never",
			want:  PullNever,
		},
	} {
		var policy PullPolicy
		require.NoError(t, yaml.Unmarshal([]byte(tc.input), &policy))
		require.Equal(t, tc.want, policy)
	}
}
