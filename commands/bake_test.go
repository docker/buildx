package commands

import (
	"testing"

	"github.com/docker/buildx/build"
	"github.com/stretchr/testify/require"
)

func TestParseExecution(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected build.Execution
	}{
		{
			name:  "default",
			input: "",
			expected: build.Execution{
				Mode: build.ExecutionModeFailFast,
			},
		},
		{
			name:  "sync output",
			input: "sync-output",
			expected: build.Execution{
				Mode: build.ExecutionModeSyncOutput,
			},
		},
		{
			name:  "keyed sync output",
			input: "mode=sync-output",
			expected: build.Execution{
				Mode: build.ExecutionModeSyncOutput,
			},
		},
		{
			name:  "defer error with parallelism",
			input: "defer-error,parallel=2",
			expected: build.Execution{
				Mode:     build.ExecutionModeDeferError,
				Parallel: 2,
			},
		},
		{
			name:  "parallelism with default mode",
			input: "parallel=1",
			expected: build.Execution{
				Mode:     build.ExecutionModeFailFast,
				Parallel: 1,
			},
		},
		{
			name:  "parallelism disabled explicitly",
			input: "parallel=0",
			expected: build.Execution{
				Mode: build.ExecutionModeFailFast,
			},
		},
		{
			name:  "whitespace",
			input: " defer-error , parallel = 2 ",
			expected: build.Execution{
				Mode:     build.ExecutionModeDeferError,
				Parallel: 2,
			},
		},
		{
			name:  "quoted value",
			input: `"mode=sync-output"`,
			expected: build.Execution{
				Mode: build.ExecutionModeSyncOutput,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, err := parseExecution(tt.input)
			require.NoError(t, err)
			require.Equal(t, tt.expected, actual)
		})
	}
}

func TestParseExecutionInvalid(t *testing.T) {
	tests := []string{
		"unknown",
		"Sync-Output",
		"mode=unknown",
		"parallel=-1",
		"parallel=nope",
		"parallel=1,parallel=2",
		"unexpected=value",
		"fail-fast,defer-error",
		"fail-fast,mode=sync-output",
	}

	for _, tt := range tests {
		t.Run(tt, func(t *testing.T) {
			_, err := parseExecution(tt)
			require.Error(t, err)
		})
	}
}
