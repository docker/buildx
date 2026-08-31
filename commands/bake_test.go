package commands

import (
	"testing"

	"github.com/docker/cli/cli/command"
	"github.com/stretchr/testify/require"
)

func TestBakeJobsFlag(t *testing.T) {
	tests := []struct {
		name string
		args []string
		jobs int
	}{
		{name: "default"},
		{name: "long", args: []string{"--jobs=2"}, jobs: 2},
		{name: "short", args: []string{"-j=2"}, jobs: 2},
		{name: "attached", args: []string{"-j2"}, jobs: 2},
		{name: "unlimited", args: []string{"-j=0"}},
		{name: "with mode", args: []string{"--execution=defer-error", "-j=2"}, jobs: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dockerCli, err := command.NewDockerCli()
			require.NoError(t, err)
			cmd := bakeCmd(dockerCli, &rootOptions{})
			require.NoError(t, cmd.ParseFlags(tt.args))
			jobs, err := cmd.Flags().GetInt("jobs")
			require.NoError(t, err)
			require.Equal(t, tt.jobs, jobs)
		})
	}
}

func TestBakeJobsNegative(t *testing.T) {
	dockerCli, err := command.NewDockerCli()
	require.NoError(t, err)
	cmd := bakeCmd(dockerCli, &rootOptions{})
	require.NoError(t, cmd.ParseFlags([]string{"-j=-1"}))
	require.EqualError(t, cmd.RunE(cmd, nil), "jobs must be non-negative")
}
