package build

import (
	"io/fs"
	"testing"

	"github.com/docker/buildx/policy"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

func boolPtr(v bool) *bool {
	return &v
}

func levelPtr(v logrus.Level) *logrus.Level {
	return &v
}

// TestWithPolicyConfigDefaults ensures default policy is returned when no configs are provided.
func TestWithPolicyConfigDefaults(t *testing.T) {
	defaultPolicy := policyOpt{
		Files: []policy.File{
			{Filename: "default.rego", Data: []byte("package policy")},
		},
		FS: func() (fs.StatFS, func() error, error) {
			return nil, nil, nil
		},
	}

	out, err := withPolicyConfig(defaultPolicy, nil)
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Equal(t, defaultPolicy.Files, out[0].Files)
	require.False(t, out[0].Strict)
	require.Nil(t, out[0].LogLevel)
	require.NotNil(t, out[0].FS)
}

// TestWithPolicyConfigDisabled validates disabled policy behavior across invalid and valid combinations.
func TestWithPolicyConfigDisabled(t *testing.T) {
	_, err := withPolicyConfig(policyOpt{}, []PolicyConfig{
		{Disabled: true, Files: []policy.File{{Filename: "x.rego"}}},
	})
	require.Error(t, err)

	_, err = withPolicyConfig(policyOpt{}, []PolicyConfig{
		{Disabled: true, Reset: true},
	})
	require.Error(t, err)

	_, err = withPolicyConfig(policyOpt{}, []PolicyConfig{
		{Disabled: true, Strict: boolPtr(true)},
	})
	require.Error(t, err)

	_, err = withPolicyConfig(policyOpt{}, []PolicyConfig{
		{Disabled: true, LogLevel: levelPtr(logrus.WarnLevel)},
	})
	require.Error(t, err)

	_, err = withPolicyConfig(policyOpt{}, []PolicyConfig{
		{Disabled: true},
		{},
	})
	require.Error(t, err)

	out, err := withPolicyConfig(policyOpt{}, []PolicyConfig{
		{Disabled: true},
	})
	require.NoError(t, err)
	require.Nil(t, out)
}

// TestWithPolicyConfigResetAndFiles ensures reset drops defaults and uses explicitly provided files.
func TestWithPolicyConfigResetAndFiles(t *testing.T) {
	defaultPolicy := policyOpt{
		Files: []policy.File{{Filename: "default.rego"}},
		FS: func() (fs.StatFS, func() error, error) {
			return nil, nil, nil
		},
	}

	out, err := withPolicyConfig(defaultPolicy, []PolicyConfig{
		{Reset: true},
		{Files: []policy.File{{Filename: "a.rego"}}},
	})
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Equal(t, "a.rego", out[0].Files[0].Filename)
	require.NotNil(t, out[0].FS)
}

// TestWithPolicyConfigStrictAndLogLevel ensures strict and log level apply to existing policy.
func TestWithPolicyConfigStrictAndLogLevel(t *testing.T) {
	defaultPolicy := policyOpt{
		Files: []policy.File{{Filename: "default.rego"}},
	}

	out, err := withPolicyConfig(defaultPolicy, []PolicyConfig{
		{Strict: boolPtr(true), LogLevel: levelPtr(logrus.WarnLevel)},
	})
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.True(t, out[0].Strict)
	require.NotNil(t, out[0].LogLevel)
	require.Equal(t, logrus.WarnLevel, *out[0].LogLevel)
}

// TestWithPolicyConfigStrictIgnoredWithoutPolicy ensures strict without any policy produces no entries.
func TestWithPolicyConfigStrictIgnoredWithoutPolicy(t *testing.T) {
	out, err := withPolicyConfig(policyOpt{}, []PolicyConfig{
		{Strict: boolPtr(true)},
	})
	require.NoError(t, err)
	require.Len(t, out, 0)
}

// TestWithPolicyConfigMultipleFilesAndOverrides ensures per-entry overrides and carryover apply across multiple files.
func TestWithPolicyConfigMultipleFilesAndOverrides(t *testing.T) {
	defaultPolicy := policyOpt{
		Files: []policy.File{{Filename: "default.rego"}},
		FS: func() (fs.StatFS, func() error, error) {
			return nil, nil, nil
		},
	}

	out, err := withPolicyConfig(defaultPolicy, []PolicyConfig{
		{Files: []policy.File{{Filename: "a.rego"}}},
		{Strict: boolPtr(true), LogLevel: levelPtr(logrus.WarnLevel)},
		{Files: []policy.File{{Filename: "b.rego"}}, Strict: boolPtr(true)},
	})
	require.NoError(t, err)
	require.Len(t, out, 3)
	require.Equal(t, "default.rego", out[0].Files[0].Filename)
	require.Equal(t, "a.rego", out[1].Files[0].Filename)
	require.True(t, out[1].Strict)
	require.NotNil(t, out[1].LogLevel)
	require.Equal(t, logrus.WarnLevel, *out[1].LogLevel)
	require.Equal(t, "b.rego", out[2].Files[0].Filename)
	require.True(t, out[2].Strict)
	require.NotNil(t, out[1].FS)
	require.NotNil(t, out[2].FS)
}
