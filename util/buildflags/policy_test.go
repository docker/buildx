package buildflags

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/docker/buildx/policy"
	gwpb "github.com/moby/buildkit/frontend/gateway/pb"
	"github.com/moby/buildkit/sourcepolicy/policysession"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	"github.com/zclconf/go-cty/cty"
)

func TestPolicyConfigMarshalJSON(t *testing.T) {
	for _, cfg := range []PolicyConfig{
		{},
		{Callback: policysession.PolicyCallback(func(_ context.Context, _ *policysession.CheckPolicyRequest) (*policysession.DecisionResponse, *gwpb.ResolveSourceMetaRequest, error) {
			return nil, nil, nil
		})},
	} {
		dt, err := json.Marshal(cfg)
		require.NoError(t, err)
		require.NotContains(t, string(dt), "Callback")
	}
}

func TestPolicyConfigs_FromCtyValue(t *testing.T) {
	policyDir := t.TempDir()
	policyPath := filepath.Join(policyDir, "policy.rego")
	require.NoError(t, os.WriteFile(policyPath, []byte("package docker\n"), 0o600))

	in := cty.TupleVal([]cty.Value{
		cty.ObjectVal(map[string]cty.Value{
			"filename":  cty.StringVal(policyPath),
			"reset":     cty.BoolVal(true),
			"strict":    cty.BoolVal(true),
			"log-level": cty.StringVal("warn"),
		}),
		cty.StringVal("filename=" + policyPath + ",disabled=true"),
	})

	var actual PolicyConfigs
	err := actual.FromCtyValue(in, nil)
	require.NoError(t, err)
	require.Len(t, actual, 2)

	require.Equal(t, policyPath, actual[0].Files[0].Filename)
	require.Nil(t, actual[0].Files[0].Data)
	require.True(t, actual[0].Reset)
	require.NotNil(t, actual[0].Strict)
	require.True(t, *actual[0].Strict)
	require.NotNil(t, actual[0].LogLevel)
	require.Equal(t, logrus.WarnLevel, *actual[0].LogLevel)

	require.Equal(t, policyPath, actual[1].Files[0].Filename)
	require.Nil(t, actual[1].Files[0].Data)
	require.True(t, actual[1].Disabled)
}

func TestPolicyConfigs_ToCtyValue(t *testing.T) {
	lvl := logrus.InfoLevel
	strict := true
	in := PolicyConfigs{
		{
			Files: []policy.File{{Filename: "a.rego"}},
			Reset: true,
		},
		{
			Files:    []policy.File{{Filename: "b.rego"}},
			Disabled: true,
			Strict:   &strict,
			LogLevel: &lvl,
		},
	}

	actual := in.ToCtyValue()
	expected := cty.ListVal([]cty.Value{
		cty.MapVal(map[string]cty.Value{
			"filename": cty.StringVal("a.rego"),
			"reset":    cty.StringVal("true"),
		}),
		cty.MapVal(map[string]cty.Value{
			"filename":  cty.StringVal("b.rego"),
			"disabled":  cty.StringVal("true"),
			"strict":    cty.StringVal("true"),
			"log-level": cty.StringVal("info"),
		}),
	})

	result := actual.Equals(expected)
	require.True(t, result.True())
}

func TestPolicyConfig_FromCtyValue(t *testing.T) {
	policyDir := t.TempDir()
	policyPath := filepath.Join(policyDir, "policy.rego")
	require.NoError(t, os.WriteFile(policyPath, []byte("package docker\n"), 0o600))

	var actual PolicyConfig
	err := actual.FromCtyValue(cty.StringVal("filename="+policyPath+",disabled=true"), nil)
	require.NoError(t, err)
	require.Equal(t, policyPath, actual.Files[0].Filename)
	require.Nil(t, actual.Files[0].Data)
	require.True(t, actual.Disabled)
}
