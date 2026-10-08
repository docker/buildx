package progress

import (
	"testing"

	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/solver/pb"
	"github.com/stretchr/testify/require"
)

func TestDedupWarnings(t *testing.T) {
	newWarning := func() client.VertexWarning {
		return client.VertexWarning{
			Vertex: "first",
			Level:  1,
			Short:  []byte("warning"),
			Detail: [][]byte{[]byte("detail"), []byte("more detail")},
			URL:    "https://example.com",
			SourceInfo: &pb.SourceInfo{
				Filename:   "Dockerfile",
				Language:   "dockerfile",
				Data:       []byte("FROM scratch"),
				Definition: &pb.Definition{Def: [][]byte{[]byte("definition")}},
			},
			Range: []*pb.Range{{Start: &pb.Position{Line: 1}, End: &pb.Position{Line: 2}}},
		}
	}

	t.Run("duplicates preserve first warning and inputs", func(t *testing.T) {
		first, second := newWarning(), newWarning()
		second.Vertex = "second"
		second.SourceInfo.Definition = &pb.Definition{Def: [][]byte{[]byte("other definition")}}
		got := dedupWarnings([]client.VertexWarning{first, second, first})
		require.Len(t, got, 1)
		require.Equal(t, first, got[0])
		require.Equal(t, [][]byte{[]byte("definition")}, first.SourceInfo.Definition.Def)
		require.Equal(t, [][]byte{[]byte("other definition")}, second.SourceInfo.Definition.Def)
	})

	for _, tc := range []struct {
		name   string
		change func(*client.VertexWarning)
	}{
		{"level", func(w *client.VertexWarning) { w.Level++ }},
		{"short", func(w *client.VertexWarning) { w.Short[0]++ }},
		{"detail", func(w *client.VertexWarning) { w.Detail[0][0]++ }},
		{"detail order", func(w *client.VertexWarning) { w.Detail[0], w.Detail[1] = w.Detail[1], w.Detail[0] }},
		{"url", func(w *client.VertexWarning) { w.URL += "/other" }},
		{"filename", func(w *client.VertexWarning) { w.SourceInfo.Filename += ".other" }},
		{"language", func(w *client.VertexWarning) { w.SourceInfo.Language = "other" }},
		{"source data", func(w *client.VertexWarning) { w.SourceInfo.Data[0]++ }},
		{"missing source", func(w *client.VertexWarning) { w.SourceInfo = nil }},
		{"range start", func(w *client.VertexWarning) { w.Range[0].Start.Line++ }},
		{"range end", func(w *client.VertexWarning) { w.Range[0].End.Character++ }},
		{"missing range", func(w *client.VertexWarning) { w.Range = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			first, second := newWarning(), newWarning()
			tc.change(&second)
			require.Equal(t, []client.VertexWarning{first, second}, dedupWarnings([]client.VertexWarning{first, second, first}))
		})
	}

	t.Run("empty values", func(t *testing.T) {
		require.Empty(t, dedupWarnings(nil))
		require.Len(t, dedupWarnings([]client.VertexWarning{{}, {Short: []byte{}, Detail: [][]byte{}, Range: []*pb.Range{}}}), 1)
		require.Len(t, dedupWarnings([]client.VertexWarning{{}, {SourceInfo: &pb.SourceInfo{}}}), 2)
	})
}
