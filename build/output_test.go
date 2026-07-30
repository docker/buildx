package build

import (
	"testing"

	"github.com/moby/buildkit/client"
	"github.com/stretchr/testify/assert"
)

func TestShouldWarnNoOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		opt         Options
		reqs        []*reqForNode
		wantWarning bool
	}{
		{
			name:        "NoRequests",
			wantWarning: true,
		},
		{
			name: "NoOutput",
			reqs: []*reqForNode{{
				so: &client.SolveOpt{},
			}},
			wantWarning: true,
		},
		{
			name: "Exporter",
			reqs: []*reqForNode{{
				so: &client.SolveOpt{
					Exports: []client.ExportEntry{{Type: "image"}},
				},
			}},
		},
		{
			name: "SessionExporter",
			reqs: []*reqForNode{{
				so: &client.SolveOpt{
					EnableSessionExporter: true,
				},
			}},
		},
		{
			name: "OutputOnOneNode",
			reqs: []*reqForNode{
				{
					so: &client.SolveOpt{},
				},
				{
					so: &client.SolveOpt{
						Exports: []client.ExportEntry{{Type: "image"}},
					},
				},
			},
		},
		{
			name: "Linked",
			opt: Options{
				Linked: true,
			},
		},
		{
			name: "ExplicitCacheOnly",
			opt: Options{
				Exports: []client.ExportEntry{{Type: "cacheonly"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.wantWarning, shouldWarnNoOutput(tt.opt, tt.reqs))
		})
	}
}
