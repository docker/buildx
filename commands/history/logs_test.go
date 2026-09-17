package history

import (
	"bytes"
	"context"
	"testing"

	controlapi "github.com/moby/buildkit/api/services/control"
	"github.com/moby/buildkit/util/progress/progressui"
	"github.com/stretchr/testify/require"
	spb "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
)

func TestLoadErrorOutput(t *testing.T) {
	t.Run("no error", func(t *testing.T) {
		out, err := loadErrorOutput(context.Background(), nil, &historyRecord{
			BuildHistoryRecord: &controlapi.BuildHistoryRecord{},
		})
		require.NoError(t, err)
		require.Nil(t, out)
	})

	t.Run("record error", func(t *testing.T) {
		out, err := loadErrorOutput(context.Background(), nil, &historyRecord{
			BuildHistoryRecord: &controlapi.BuildHistoryRecord{
				Error: &spb.Status{
					Code:    int32(codes.Internal),
					Message: "failed to solve",
				},
			},
		})
		require.NoError(t, err)
		require.Equal(t, &errorOutput{
			Code:    int(codes.Internal),
			Message: "failed to solve",
		}, out)
	})
}

func TestPrintLogError(t *testing.T) {
	for _, tt := range []struct {
		name string
		mode progressui.DisplayMode
		in   *errorOutput
		want string
	}{
		{
			name: "none",
		},
		{
			name: "error",
			mode: progressui.PlainMode,
			in: &errorOutput{
				Code:    int(codes.Internal),
				Message: "failed to solve",
			},
			want: "\nError: Internal failed to solve\n",
		},
		{
			name: "canceled",
			mode: progressui.PlainMode,
			in: &errorOutput{
				Code:    int(codes.Canceled),
				Message: "context canceled",
			},
			want: "\nBuild canceled\n",
		},
		{
			name: "details",
			mode: progressui.PlainMode,
			in: &errorOutput{
				Code:    int(codes.Internal),
				Message: "failed to solve",
				Name:    "[1/1] RUN exit 1",
				Logs:    []string{"process exited with code 1"},
				Sources: []byte("Dockerfile:1\n"),
			},
			want: "\nError: Internal failed to solve\nDockerfile:1\nLogs:\n> => [1/1] RUN exit 1:\n> process exited with code 1\n\n",
		},
		{
			name: "rawjson",
			mode: progressui.RawJSONMode,
			in: &errorOutput{
				Code:    int(codes.Internal),
				Message: "failed to solve",
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			printLogError(&buf, tt.mode, tt.in)
			require.Equal(t, tt.want, buf.String())
		})
	}
}
