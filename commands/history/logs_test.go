package history

import (
	"bytes"
	"context"
	"testing"

	controlapi "github.com/moby/buildkit/api/services/control"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	spb "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
)

func TestLoadBuildErrorOutput_NoError(t *testing.T) {
	rec := &historyRecord{
		BuildHistoryRecord: &controlapi.BuildHistoryRecord{},
	}
	out, err := loadBuildErrorOutput(context.Background(), nil, rec)
	require.NoError(t, err)
	assert.Nil(t, out)
}

func TestLoadBuildErrorOutput_GRPCError(t *testing.T) {
	rec := &historyRecord{
		BuildHistoryRecord: &controlapi.BuildHistoryRecord{
			Error: &spb.Status{
				Code:    int32(codes.Internal),
				Message: "failed to solve: process did not complete successfully",
			},
		},
	}
	out, err := loadBuildErrorOutput(context.Background(), nil, rec)
	require.NoError(t, err)
	require.NotNil(t, out)
	assert.Equal(t, int(codes.Internal), out.Code)
	assert.Equal(t, "failed to solve: process did not complete successfully", out.Message)
	assert.Nil(t, out.Sources)
	assert.Empty(t, out.Logs)
}

func TestLoadBuildErrorOutput_CanceledError(t *testing.T) {
	rec := &historyRecord{
		BuildHistoryRecord: &controlapi.BuildHistoryRecord{
			Error: &spb.Status{
				Code:    int32(codes.Canceled),
				Message: "context canceled",
			},
		},
	}
	out, err := loadBuildErrorOutput(context.Background(), nil, rec)
	require.NoError(t, err)
	require.NotNil(t, out)
	assert.Equal(t, int(codes.Canceled), out.Code)
}

func TestPrintLogsError_Nil(t *testing.T) {
	var buf bytes.Buffer
	printLogsError(&buf, nil)
	assert.Empty(t, buf.String())
}

func TestPrintLogsError_GRPCError(t *testing.T) {
	var buf bytes.Buffer
	printLogsError(&buf, &errorOutput{
		Code:    int(codes.Internal),
		Message: "failed to solve: dockerfile parse error",
	})
	out := buf.String()
	assert.Contains(t, out, "Error: Internal failed to solve: dockerfile parse error")
}

func TestPrintLogsError_CanceledError(t *testing.T) {
	var buf bytes.Buffer
	printLogsError(&buf, &errorOutput{
		Code: int(codes.Canceled),
	})
	out := buf.String()
	assert.Contains(t, out, "Build canceled")
	assert.NotContains(t, out, "Error:")
}

func TestPrintLogsError_WithSources(t *testing.T) {
	var buf bytes.Buffer
	printLogsError(&buf, &errorOutput{
		Code:    int(codes.Internal),
		Message: "failed to solve",
		Sources: []byte("Dockerfile:5\n > 5: RUN exit 1\n"),
	})
	out := buf.String()
	assert.Contains(t, out, "Error: Internal failed to solve")
	assert.Contains(t, out, "Dockerfile:5")
	assert.Contains(t, out, "RUN exit 1")
}

func TestPrintLogsError_WithLogs(t *testing.T) {
	var buf bytes.Buffer
	printLogsError(&buf, &errorOutput{
		Code:    int(codes.Internal),
		Message: "failed to solve",
		Name:    "RUN echo hello",
		Logs:    []string{"hello", "world"},
	})
	out := buf.String()
	assert.Contains(t, out, "Logs:")
	assert.Contains(t, out, "> => RUN echo hello:")
	assert.Contains(t, out, "> hello")
	assert.Contains(t, out, "> world")
}

func TestPrintErrorDetails_SourcesLogsStack(t *testing.T) {
	var buf bytes.Buffer
	printErrorDetails(&buf, &errorOutput{
		Sources: []byte("Dockerfile:5\n > 5: RUN exit 1\n"),
		Name:    "RUN exit 1",
		Logs:    []string{"step output"},
		Stack:   []byte("goroutine 1 [running]:\n..."),
	})
	out := buf.String()
	assert.Contains(t, out, "Dockerfile:5")
	assert.Contains(t, out, "Logs:")
	assert.Contains(t, out, "> step output")
	assert.Contains(t, out, "Enable --debug to see stack traces for error")
	// header line is not printed by printErrorDetails
	assert.NotContains(t, out, "Error:")
	assert.NotContains(t, out, "Build canceled")
}

func TestPrintLogsError_StackWithoutDebug(t *testing.T) {
	var buf bytes.Buffer
	printLogsError(&buf, &errorOutput{
		Code:    int(codes.Internal),
		Message: "failed to solve",
		Stack:   []byte("goroutine 1 [running]:\n..."),
	})
	out := buf.String()
	// debug is not enabled in tests, so we should see the hint
	assert.Contains(t, out, "Enable --debug to see stack traces for error")
	assert.NotContains(t, out, "goroutine 1")
}
