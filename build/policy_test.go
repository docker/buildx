package build

import (
	"testing"

	"github.com/docker/buildx/policy"
	"github.com/docker/buildx/util/buildflags"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

// TestWithPolicyConfigDefaults ensures default policy is returned when no configs are provided.
func TestWithPolicyConfigDefaults(t *testing.T) {
	defaultPolicy := policyOpt{
		Files: []policyFileSpec{
			{Filename: "default.rego", Optional: true},
		},
	}

	out, err := withPolicyConfig(defaultPolicy, nil)
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Equal(t, defaultPolicy.Files, out[0].Files)
	require.False(t, out[0].Strict)
	require.Nil(t, out[0].LogLevel)
}

// TestWithPolicyConfigDisabled validates disabled policy behavior across invalid and valid combinations.
func TestWithPolicyConfigDisabled(t *testing.T) {
	_, err := withPolicyConfig(policyOpt{}, []buildflags.PolicyConfig{
		{Disabled: true, Files: []policy.File{{Filename: "x.rego"}}},
	})
	require.Error(t, err)

	_, err = withPolicyConfig(policyOpt{}, []buildflags.PolicyConfig{
		{Disabled: true, Reset: true},
	})
	require.Error(t, err)

	_, err = withPolicyConfig(policyOpt{}, []buildflags.PolicyConfig{
		{Disabled: true, Strict: new(true)},
	})
	require.Error(t, err)

	_, err = withPolicyConfig(policyOpt{}, []buildflags.PolicyConfig{
		{Disabled: true, LogLevel: new(logrus.WarnLevel)},
	})
	require.Error(t, err)

	_, err = withPolicyConfig(policyOpt{}, []buildflags.PolicyConfig{
		{Disabled: true},
		{},
	})
	require.Error(t, err)

	out, err := withPolicyConfig(policyOpt{}, []buildflags.PolicyConfig{
		{Disabled: true},
	})
	require.NoError(t, err)
	require.Nil(t, out)
}

// TestWithPolicyConfigResetAndFiles ensures reset drops defaults and uses explicitly provided files.
func TestWithPolicyConfigResetAndFiles(t *testing.T) {
	defaultPolicy := policyOpt{
		Files: []policyFileSpec{{Filename: "default.rego", Optional: true}},
	}

	out, err := withPolicyConfig(defaultPolicy, []buildflags.PolicyConfig{
		{Reset: true},
		{Files: []policy.File{{Filename: "a.rego"}}},
	})
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Equal(t, "a.rego", out[0].Files[0].Filename)
	require.False(t, out[0].Files[0].Optional)
}

// TestWithPolicyConfigStrictAndLogLevel ensures strict and log level apply to existing policy.
func TestWithPolicyConfigStrictAndLogLevel(t *testing.T) {
	defaultPolicy := policyOpt{
		Files: []policyFileSpec{{Filename: "default.rego", Optional: true}},
	}

	out, err := withPolicyConfig(defaultPolicy, []buildflags.PolicyConfig{
		{Strict: new(true), LogLevel: new(logrus.WarnLevel)},
	})
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.True(t, out[0].Strict)
	require.NotNil(t, out[0].LogLevel)
	require.Equal(t, logrus.WarnLevel, *out[0].LogLevel)
}

// TestWithPolicyConfigStrictIgnoredWithoutPolicy ensures strict without any policy produces no entries.
func TestWithPolicyConfigStrictIgnoredWithoutPolicy(t *testing.T) {
	out, err := withPolicyConfig(policyOpt{}, []buildflags.PolicyConfig{
		{Strict: new(true)},
	})
	require.NoError(t, err)
	require.Len(t, out, 0)
}

// TestWithPolicyConfigMultipleFilesAndOverrides ensures per-entry overrides and carryover apply across multiple files.
func TestWithPolicyConfigMultipleFilesAndOverrides(t *testing.T) {
	defaultPolicy := policyOpt{
		Files: []policyFileSpec{{Filename: "default.rego", Optional: true}},
	}

	out, err := withPolicyConfig(defaultPolicy, []buildflags.PolicyConfig{
		{Files: []policy.File{{Filename: "a.rego"}}},
		{Strict: new(true), LogLevel: new(logrus.WarnLevel)},
		{Files: []policy.File{{Filename: "b.rego"}}, Strict: new(true)},
	})
	require.NoError(t, err)
	require.Len(t, out, 3)
	require.Equal(t, "default.rego", out[0].Files[0].Filename)
	require.True(t, out[0].Files[0].Optional)
	require.Equal(t, "a.rego", out[1].Files[0].Filename)
	require.False(t, out[1].Files[0].Optional)
	require.True(t, out[1].Strict)
	require.NotNil(t, out[1].LogLevel)
	require.Equal(t, logrus.WarnLevel, *out[1].LogLevel)
	require.Equal(t, "b.rego", out[2].Files[0].Filename)
	require.False(t, out[2].Files[0].Optional)
	require.True(t, out[2].Strict)
}
