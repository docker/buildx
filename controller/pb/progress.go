package pb

import (
	"time"

	"github.com/docker/buildx/util/progress"
	control "github.com/moby/buildkit/api/services/control"
	"github.com/moby/buildkit/client"
	"github.com/opencontainers/go-digest"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type writer struct {
	ch chan<- *StatusResponse
}

func NewProgressWriter(ch chan<- *StatusResponse) progress.Writer {
	return &writer{ch: ch}
}

func (w *writer) Write(status *client.SolveStatus) {
	w.ch <- ToControlStatus(status)
}

func (w *writer) WriteBuildRef(target string, ref string) {}

func (w *writer) ValidateLogSource(digest.Digest, interface{}) bool {
	return true
}

func (w *writer) ClearLogSource(interface{}) {}

func ToControlStatus(s *client.SolveStatus) *StatusResponse {
	resp := StatusResponse{}
	for _, v := range s.Vertexes {
		resp.Vertexes = append(resp.Vertexes, &control.Vertex{
			Digest:        string(v.Digest),
			Inputs:        digestSliceToPB(v.Inputs),
			Name:          v.Name,
			Started:       timestampToPB(v.Started),
			Completed:     timestampToPB(v.Completed),
			Error:         v.Error,
			Cached:        v.Cached,
			ProgressGroup: v.ProgressGroup,
		})
	}
	for _, v := range s.Statuses {
		resp.Statuses = append(resp.Statuses, &control.VertexStatus{
			ID:        v.ID,
			Vertex:    string(v.Vertex),
			Name:      v.Name,
			Total:     v.Total,
			Current:   v.Current,
			Timestamp: timestamppb.New(v.Timestamp),
			Started:   timestampToPB(v.Started),
			Completed: timestampToPB(v.Completed),
		})
	}
	for _, v := range s.Logs {
		resp.Logs = append(resp.Logs, &control.VertexLog{
			Vertex:    string(v.Vertex),
			Stream:    int64(v.Stream),
			Msg:       v.Data,
			Timestamp: timestamppb.New(v.Timestamp),
		})
	}
	for _, v := range s.Warnings {
		resp.Warnings = append(resp.Warnings, &control.VertexWarning{
			Vertex: string(v.Vertex),
			Level:  int64(v.Level),
			Short:  v.Short,
			Detail: v.Detail,
			Url:    v.URL,
			Info:   v.SourceInfo,
			Ranges: v.Range,
		})
	}
	return &resp
}

func FromControlStatus(resp *StatusResponse) *client.SolveStatus {
	s := client.SolveStatus{}
	for _, v := range resp.Vertexes {
		s.Vertexes = append(s.Vertexes, &client.Vertex{
			Digest:        digest.Digest(v.Digest),
			Inputs:        digestSliceFromPB(v.Inputs),
			Name:          v.Name,
			Started:       timestampFromPB(v.Started),
			Completed:     timestampFromPB(v.Completed),
			Error:         v.Error,
			Cached:        v.Cached,
			ProgressGroup: v.ProgressGroup,
		})
	}
	for _, v := range resp.Statuses {
		s.Statuses = append(s.Statuses, &client.VertexStatus{
			ID:        v.ID,
			Vertex:    digest.Digest(v.Vertex),
			Name:      v.Name,
			Total:     v.Total,
			Current:   v.Current,
			Timestamp: v.Timestamp.AsTime(),
			Started:   timestampFromPB(v.Started),
			Completed: timestampFromPB(v.Completed),
		})
	}
	for _, v := range resp.Logs {
		s.Logs = append(s.Logs, &client.VertexLog{
			Vertex:    digest.Digest(v.Vertex),
			Stream:    int(v.Stream),
			Data:      v.Msg,
			Timestamp: v.Timestamp.AsTime(),
		})
	}
	for _, v := range resp.Warnings {
		s.Warnings = append(s.Warnings, &client.VertexWarning{
			Vertex:     digest.Digest(v.Vertex),
			Level:      int(v.Level),
			Short:      v.Short,
			Detail:     v.Detail,
			URL:        v.Url,
			SourceInfo: v.Info,
			Range:      v.Ranges,
		})
	}
	return &s
}

func timestampFromPB(ts *timestamppb.Timestamp) *time.Time {
	if ts == nil {
		return nil
	}

	t := ts.AsTime()
	if t.IsZero() {
		return nil
	}
	return &t
}

func timestampToPB(ts *time.Time) *timestamppb.Timestamp {
	if ts == nil {
		return nil
	}
	return timestamppb.New(*ts)
}

func digestSliceFromPB(elems []string) []digest.Digest {
	clone := make([]digest.Digest, len(elems))
	for i, e := range elems {
		clone[i] = digest.Digest(e)
	}
	return clone
}

func digestSliceToPB(elems []digest.Digest) []string {
	clone := make([]string, len(elems))
	for i, e := range elems {
		clone[i] = string(e)
	}
	return clone
}
