package replay

import (
	"sync"

	"github.com/docker/buildx/util/progress"
)

// warnOnce routes non-fatal warnings through a progress sub-logger
// and drops duplicates by key. It is shared across snapshot targets so a
// predicate that references the same material from every platform warns
// at most once.
type warnOnce struct {
	mu      sync.Mutex
	visited map[string]struct{}
}

func newWarnOnce() *warnOnce {
	return &warnOnce{visited: map[string]struct{}{}}
}

// Log emits msg (prefixed with "warning: ") on sub's stderr stream the
// first time key is seen. Subsequent calls with the same key are silent.
// sub may be nil, in which case the message is dropped entirely.
func (w *warnOnce) Log(sub progress.SubLogger, key, msg string) {
	if w == nil {
		return
	}
	w.mu.Lock()
	if _, ok := w.visited[key]; ok {
		w.mu.Unlock()
		return
	}
	w.visited[key] = struct{}{}
	w.mu.Unlock()
	if sub == nil {
		return
	}
	sub.Log(2, []byte("warning: "+msg+"\n"))
}
