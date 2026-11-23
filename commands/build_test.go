package commands

import (
	"testing"

	"github.com/docker/buildx/util/buildflags"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
)

func TestPullFlag(t *testing.T) {
	for _, tc := range []struct {
		name      string
		args      []string
		want      buildflags.PullPolicy
		noCache   bool
		wantError bool
	}{
		{
			name: "default",
			args: []string{"."},
		},
		{
			name: "bare before context",
			args: []string{"--pull", "."},
			want: "true",
		},
		{
			name:    "bare before flag",
			args:    []string{"--pull", "--no-cache", "."},
			want:    "true",
			noCache: true,
		},
		{
			name: "boolean false",
			args: []string{"--pull=false", "."},
			want: "false",
		},
		{
			name: "always",
			args: []string{"--pull=always", "."},
			want: buildflags.PullAlways,
		},
		{
			name: "missing",
			args: []string{"--pull=missing", "."},
			want: buildflags.PullMissing,
		},
		{
			name: "never",
			args: []string{"--pull=never", "."},
			want: buildflags.PullNever,
		},
		{
			name:      "invalid",
			args:      []string{"--pull=invalid", "."},
			wantError: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			flags := pflag.NewFlagSet("build", pflag.ContinueOnError)
			var opts commonFlags
			commonBuildFlags(&opts, flags)
			err := flags.Parse(tc.args)
			if tc.wantError {
				require.ErrorContains(t, err, "invalid pull policy")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, *opts.pull)
			require.Equal(t, tc.noCache, *opts.noCache)
			require.Equal(t, []string{"."}, flags.Args())
		})
	}
}
