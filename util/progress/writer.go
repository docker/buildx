package progress

import (
	"github.com/moby/buildkit/client"
)

type Writer interface {
	Done() <-chan struct{}
	Err() error
	Status() chan *client.SolveStatus
}
