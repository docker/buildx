package commands

import (
	"bytes"
	"encoding/json"
	stderrors "errors"
	"io"
	"testing"

	"github.com/docker/buildx/build"
	"github.com/docker/buildx/util/progress"
	"github.com/docker/cli/cli/command"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/util/progress/progressui"
	"github.com/stretchr/testify/require"
)

func TestWriteBakeTargetSummary(t *testing.T) {
	names := []string{"a-success", "b-failure", "c-dependent", "d-pending"}
	results := map[string]build.TargetResult{
		"a-success":   {},
		"b-failure":   {Err: stderrors.New("failed")},
		"c-dependent": {Err: stderrors.New("dependency failed"), Aborted: true},
	}
	for _, mode := range []progressui.DisplayMode{progressui.PlainMode, progressui.RawJSONMode, progressui.QuietMode} {
		t.Run(string(mode), func(t *testing.T) {
			var out bytes.Buffer
			printer, err := progress.NewPrinter(t.Context(), &out, mode)
			require.NoError(t, err)
			writeBakeTargetSummary(printer.Write, names, results)
			require.NoError(t, printer.Wait())
			switch mode {
			case progressui.PlainMode:
				require.Contains(t, out.String(), "#1 [internal] target results\n")
				for _, row := range []string{"a-success: succeeded", "b-failure: failed", "c-dependent: aborted", "d-pending: not started"} {
					require.Contains(t, out.String(), "#1 "+row+" done\n")
				}
			case progressui.RawJSONMode:
				dec := json.NewDecoder(&out)
				var vertices []*client.Vertex
				var statuses []*client.VertexStatus
				for {
					var event client.SolveStatus
					err := dec.Decode(&event)
					if err == io.EOF {
						break
					}
					require.NoError(t, err)
					vertices = append(vertices, event.Vertexes...)
					statuses = append(statuses, event.Statuses...)
				}
				require.Len(t, vertices, 2)
				require.Equal(t, "[internal] target results", vertices[0].Name)
				require.Equal(t, vertices[0].Digest, vertices[1].Digest)
				require.NotNil(t, vertices[1].Completed)
				require.Empty(t, vertices[1].Error)
				require.Len(t, statuses, len(names))
				for i, outcome := range []string{"succeeded", "failed", "aborted", "not started"} {
					require.Equal(t, vertices[0].Digest, statuses[i].Vertex)
					require.Equal(t, names[i]+": "+outcome, statuses[i].ID)
					require.Equal(t, outcome, statuses[i].Name)
					require.NotNil(t, statuses[i].Completed)
				}
			case progressui.QuietMode:
				require.Empty(t, out.String())
			}
		})
	}
}

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
