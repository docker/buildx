package pb

import (
	"github.com/docker/buildx/util/progress"
	control "github.com/moby/buildkit/api/services/control"
	"github.com/moby/buildkit/client"
	"github.com/opencontainers/go-digest"
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

func (w *writer) WriteBuildRef(target string, ref string) {
	return
}

func (w *writer) ValidateLogSource(digest.Digest, interface{}) bool {
	return true
}

func (w *writer) ClearLogSource(interface{}) {}

func ToControlStatus(s *client.SolveStatus) *StatusResponse {
	resp := StatusResponse{}
	for _, v := range s.Vertexes {
		resp.Vertexes = append(resp.Vertexes, &control.Vertex{
			Digest:        v.Digest,
			Inputs:        v.Inputs,
			Name:          v.Name,
			Started:       v.Started,
			Completed:     v.Completed,
			Error:         v.Error,
			Cached:        v.Cached,
			ProgressGroup: v.ProgressGroup,
		})
	}
	for _, v := range s.Statuses {
		resp.Statuses = append(resp.Statuses, &control.VertexStatus{
			ID:        v.ID,
			Vertex:    v.Vertex,
			Name:      v.Name,
			Total:     v.Total,
			Current:   v.Current,
			Timestamp: v.Timestamp,
			Started:   v.Started,
			Completed: v.Completed,
		})
	}
	for _, v := range s.Logs {
		resp.Logs = append(resp.Logs, &control.VertexLog{
			Vertex:    v.Vertex,
			Stream:    int64(v.Stream),
			Msg:       v.Data,
			Timestamp: v.Timestamp,
		})
	}
	for _, v := range s.Warnings {
		resp.Warnings = append(resp.Warnings, &control.VertexWarning{
			Vertex: v.Vertex,
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
			Digest:        v.Digest,
			Inputs:        v.Inputs,
			Name:          v.Name,
			Started:       v.Started,
			Completed:     v.Completed,
			Error:         v.Error,
			Cached:        v.Cached,
			ProgressGroup: v.ProgressGroup,
		})
	}
	for _, v := range resp.Statuses {
		s.Statuses = append(s.Statuses, &client.VertexStatus{
			ID:        v.ID,
			Vertex:    v.Vertex,
			Name:      v.Name,
			Total:     v.Total,
			Current:   v.Current,
			Timestamp: v.Timestamp,
			Started:   v.Started,
			Completed: v.Completed,
		})
	}
	for _, v := range resp.Logs {
		s.Logs = append(s.Logs, &client.VertexLog{
			Vertex:    v.Vertex,
			Stream:    int(v.Stream),
			Data:      v.Msg,
			Timestamp: v.Timestamp,
		})
	}
	for _, v := range resp.Warnings {
		s.Warnings = append(s.Warnings, &client.VertexWarning{
			Vertex:     v.Vertex,
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
