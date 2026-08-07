package logutil

import (
	"io"
	"sync"
	"testing"

	"github.com/sirupsen/logrus"
)

func TestPauseConcurrent(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	for range 2 {
		go func() {
			defer wg.Done()
			<-start
			Pause(logger)()
		}()
	}

	close(start)
	wg.Wait()
}
