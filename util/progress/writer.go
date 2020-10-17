package progress

import (
	"time"

	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/identity"
	"github.com/opencontainers/go-digest"
)

type Writer interface {
	Write(*client.SolveStatus)
}

func Write(w Writer, name string, f func() error) {
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

	err := f()

	tm2 := time.Now()
	vtx2 := vtx
	vtx2.Completed = &tm2
	if err != nil {
		vtx2.Error = err.Error()
	}
	w.Write(&client.SolveStatus{
		Vertexes: []*client.Vertex{&vtx2},
	})
}

func NewChannel(w Writer) (chan *client.SolveStatus, chan struct{}) {
	ch := make(chan *client.SolveStatus)
	done := make(chan struct{})
	go func() {
		for {
			v, ok := <-ch
			if !ok {
				close(done)
				return
			}
			w.Write(v)
		}
	}()
	return ch, done
}
