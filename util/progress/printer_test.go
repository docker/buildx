package progress

import (
	"sync"
	"testing"

	"github.com/moby/buildkit/client"
	"github.com/stretchr/testify/require"
)

func TestPrinterWriteAfterWait(t *testing.T) {
	p := &Printer{
		status: make(chan *client.SolveStatus),
		done:   make(chan struct{}),
	}
	go func() {
		for range p.status {
		}
		close(p.done)
	}()

	p.Write(&client.SolveStatus{})
	require.NoError(t, p.Wait())
	require.NotPanics(t, func() {
		p.Write(&client.SolveStatus{})
	})
}

func TestPrinterWriteRaceWait(t *testing.T) {
	p := &Printer{
		status: make(chan *client.SolveStatus),
		done:   make(chan struct{}),
	}
	go func() {
		for range p.status {
		}
		close(p.done)
	}()

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			require.NotPanics(t, func() {
				p.Write(&client.SolveStatus{})
			})
		}()
	}
	require.NoError(t, p.Wait())
	wg.Wait()
}
