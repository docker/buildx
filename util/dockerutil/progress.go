package dockerutil

import (
	"errors"
	"io"
	"time"

	"github.com/docker/buildx/util/progress"
	"github.com/docker/cli/cli/streams"
	"github.com/docker/docker/pkg/jsonmessage"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/identity"
	"github.com/opencontainers/go-digest"
)

func fromReader(w progress.Writer, name string, rc io.ReadCloser) error {
	dgst := digest.FromBytes([]byte(identity.NewID()))
	tm := time.Now()

	vtx := client.Vertex{
		Digest:  dgst,
		Name:    name,
		Started: &tm,
	}

	w.Write(&client.SolveStatus{
		Vertexes: []*client.Vertex{&vtx},
	})

	err := jsonmessage.DisplayJSONMessagesToStream(rc, streams.NewOut(io.Discard), nil)
	if err != nil {
		if jerr, ok := err.(*jsonmessage.JSONError); ok {
			err = errors.New(jerr.Message)
		}
	}

	tm2 := time.Now()
	vtx2 := vtx
	vtx2.Completed = &tm2
	if err != nil {
		vtx2.Error = err.Error()
	}
	w.Write(&client.SolveStatus{
		Vertexes: []*client.Vertex{&vtx2},
	})
	return err
}
