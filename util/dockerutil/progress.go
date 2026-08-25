package dockerutil

import (
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/docker/buildx/util/progress"
	"github.com/moby/buildkit/client"
	"github.com/moby/moby/api/types/jsonstream"
)

const minTimeDelta = 2 * time.Second

type progressKind int

const (
	progressKindLoad progressKind = iota
	progressKindPull
)

// loadProgressFromReader parses Docker image load JSON progress and forwards it
// to a BuildKit progress sublogger.
func loadProgressFromReader(l progress.SubLogger, rc io.ReadCloser) error {
	return progressFromReader(l, rc, progressKindLoad)
}

// PullProgressFromReader parses Docker image pull JSON progress and forwards it
// to a BuildKit progress sublogger.
func PullProgressFromReader(l progress.SubLogger, rc io.Reader) error {
	return progressFromReader(l, rc, progressKindPull)
}

func progressFromReader(l progress.SubLogger, rc io.Reader, kind progressKind) (retErr error) {
	started := map[string]client.VertexStatus{}

	defer func() {
		if retErr != nil && kind != progressKindLoad {
			return
		}
		for _, st := range started {
			if st.Completed == nil {
				now := time.Now()
				st.Completed = &now
				l.SetStatus(&st)
			}
		}
	}()

	dec := json.NewDecoder(rc)
	var parsedErr error
	for {
		var jm jsonstream.Message
		if err := dec.Decode(&jm); err != nil {
			if parsedErr != nil {
				retErr = parsedErr
				return retErr
			}
			if err == io.EOF {
				break
			}
			retErr = err
			return retErr
		}
		if jm.Error != nil {
			parsedErr = jm.Error
		}
		if jm.ID == "" {
			continue
		}

		var id string
		var start, complete, updateProgress bool
		switch kind {
		case progressKindLoad:
			if jm.Progress == nil {
				continue
			}
			id = "loading layer " + jm.ID
			start = true
			updateProgress = jm.Status == "Loading layer"
		case progressKindPull:
			if strings.ContainsRune(jm.ID, '@') || strings.ContainsRune(jm.Status, ':') || strings.HasPrefix(jm.Status, "Pulling from ") {
				continue
			}
			id = "pulling layer " + jm.ID
			start = jm.Progress != nil || strings.HasPrefix(jm.Status, "Pulling") || strings.HasPrefix(jm.Status, "Already exists")
			complete = jm.Status == "Pull complete" || jm.Status == "Already exists"
			updateProgress = jm.Progress != nil && jm.Status == "Downloading"
		}
		if !start && !complete {
			continue
		}

		st, ok := started[id]
		if !ok {
			if !start {
				continue
			}
			now := time.Now()
			st = client.VertexStatus{
				ID:      id,
				Started: &now,
			}
		}
		if updateProgress {
			st.Current = jm.Progress.Current
			st.Total = jm.Progress.Total
		}
		now := time.Now()
		if jm.Error != nil {
			st.Completed = &now
		} else if complete {
			st.Completed = &now
			st.Current = st.Total
		} else if kind == progressKindLoad {
			timeDelta := time.Since(st.Timestamp)
			if timeDelta < minTimeDelta {
				started[id] = st
				continue
			}
		}
		st.Timestamp = now
		started[id] = st
		l.SetStatus(&st)
	}

	return nil
}
