package build

import (
	"bytes"
	"testing"

	"github.com/docker/buildx/builder"
	"github.com/docker/buildx/driver"
	"github.com/moby/buildkit/client"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type warnOutputFactory struct {
	driver.Factory
	name string
}

func (f warnOutputFactory) Name() string {
	return f.name
}

type warnOutputDriver struct {
	driver.Driver
	factory driver.Factory
	moby    bool
}

func (d warnOutputDriver) Factory() driver.Factory {
	return d.factory
}

func (d warnOutputDriver) IsMobyDriver() bool {
	return d.moby
}

func TestPrepareMultiDriverExportsForEveryNode(t *testing.T) {
	const taggedName = "registry.example.com/user/app:latest"
	const secondaryName = "registry.example.com/user/secondary:latest"
	newSolveOpt := func() *client.SolveOpt {
		return &client.SolveOpt{
			Exports: []client.ExportEntry{
				{
					Type: "image",
					Attrs: map[string]string{
						"name":              taggedName,
						"push":              "true",
						"registry.insecure": "true",
					},
				},
				{
					Type: "image",
					Attrs: map[string]string{
						"name": secondaryName,
						"push": "true",
					},
				},
			},
		}
	}
	solveOpts := []*client.SolveOpt{newSolveOpt(), newSolveOpt()}

	var pushNames string
	var insecurePush bool
	for _, so := range solveOpts {
		require.NoError(t, prepareMultiDriverExports(so, &pushNames, &insecurePush))
	}

	require.Equal(t, taggedName, pushNames)
	require.True(t, insecurePush)
	for _, so := range solveOpts {
		require.Equal(t, "registry.example.com/user/app", so.Exports[0].Attrs["name"])
		require.Equal(t, "true", so.Exports[0].Attrs["push-by-digest"])
		require.Equal(t, secondaryName, so.Exports[1].Attrs["name"])
		require.NotContains(t, so.Exports[1].Attrs, "push-by-digest")
	}
}

func TestWarnOnNoOutput(t *testing.T) {
	cloudNodes := []builder.Node{{Driver: newWarnOutputDriver("cloud", false)}}
	mobyNodes := []builder.Node{{Driver: newWarnOutputDriver("docker", true)}}
	defaultOpts := map[string]Options{"default": {}}

	tests := []struct {
		name        string
		nodes       []builder.Node
		opts        map[string]Options
		reqForNodes map[string][]*reqForNode
		wantWarning string
	}{
		{
			name: "NoNonMobyDriver",
			opts: defaultOpts,
		},
		{
			name:  "MobyDriver",
			nodes: mobyNodes,
			opts:  defaultOpts,
		},
		{
			name:        "NoRequests",
			nodes:       cloudNodes,
			opts:        defaultOpts,
			wantWarning: "No output specified with cloud driver.",
		},
		{
			name:        "NoOutput",
			nodes:       cloudNodes,
			opts:        defaultOpts,
			reqForNodes: reqForDefaultTarget(&client.SolveOpt{}),
			wantWarning: "No output specified with cloud driver.",
		},
		{
			name:  "DefaultLoadPreparedExporter",
			nodes: cloudNodes,
			opts:  defaultOpts,
			reqForNodes: reqForDefaultTarget(&client.SolveOpt{
				Exports: []client.ExportEntry{{Type: "docker"}},
			}),
		},
		{
			name:  "SessionExporter",
			nodes: cloudNodes,
			opts:  defaultOpts,
			reqForNodes: reqForDefaultTarget(&client.SolveOpt{
				EnableSessionExporter: true,
			}),
		},
		{
			name:  "OutputOnOneNode",
			nodes: cloudNodes,
			opts:  defaultOpts,
			reqForNodes: reqForDefaultTarget(
				&client.SolveOpt{},
				&client.SolveOpt{Exports: []client.ExportEntry{{Type: "image"}}},
			),
		},
		{
			name:  "Linked",
			nodes: cloudNodes,
			opts:  map[string]Options{"default": {Linked: true}},
		},
		{
			name:  "ExplicitCacheOnly",
			nodes: cloudNodes,
			opts: map[string]Options{"default": {
				Exports: []client.ExportEntry{{Type: "cacheonly"}},
			}},
		},
		{
			name:  "CallFunc",
			nodes: cloudNodes,
			opts:  map[string]Options{"default": {CallFunc: &CallFunc{Name: "outline"}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("BUILDX_NO_DEFAULT_LOAD", "false")

			var buf bytes.Buffer
			restoreLogrus := captureLogrusWarnings(&buf)
			defer restoreLogrus()

			warnOnNoOutput(tt.nodes, tt.opts, tt.reqForNodes)

			if tt.wantWarning == "" {
				assert.Empty(t, buf.String())
				return
			}
			assert.Contains(t, buf.String(), tt.wantWarning)
			assert.Contains(t, buf.String(), "Build result will only remain in the build cache.")
		})
	}
}

func reqForDefaultTarget(solveOpts ...*client.SolveOpt) map[string][]*reqForNode {
	reqs := make([]*reqForNode, 0, len(solveOpts))
	for _, so := range solveOpts {
		reqs = append(reqs, &reqForNode{so: so})
	}
	return map[string][]*reqForNode{"default": reqs}
}

func newWarnOutputDriver(name string, moby bool) *driver.DriverHandle {
	return &driver.DriverHandle{
		Driver: warnOutputDriver{
			factory: warnOutputFactory{name: name},
			moby:    moby,
		},
	}
}

func captureLogrusWarnings(w *bytes.Buffer) func() {
	logger := logrus.StandardLogger()
	oldOut := logger.Out
	oldFormatter := logger.Formatter
	oldLevel := logger.Level

	logger.SetOutput(w)
	logger.SetFormatter(&logrus.TextFormatter{DisableTimestamp: true, DisableColors: true})
	logger.SetLevel(logrus.WarnLevel)

	return func() {
		logger.SetOutput(oldOut)
		logger.SetFormatter(oldFormatter)
		logger.SetLevel(oldLevel)
	}
}

func TestWarnOnNoOutputDisabledByEnv(t *testing.T) {
	t.Setenv("BUILDX_NO_DEFAULT_LOAD", "true")

	var buf bytes.Buffer
	restoreLogrus := captureLogrusWarnings(&buf)
	defer restoreLogrus()

	warnOnNoOutput([]builder.Node{{
		Driver: newWarnOutputDriver("cloud", false),
	}}, map[string]Options{
		"default": {},
	}, nil)

	require.Empty(t, buf.String())
}
