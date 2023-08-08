package dockerutil

import (
	"encoding/json"
	"io"
	"time"

	"github.com/docker/buildx/util/progress"
	"github.com/docker/docker/pkg/jsonmessage"
	"github.com/moby/buildkit/client"
)

const minTimeDelta = 2 * time.Second

func fromReader(l progress.SubLogger, rc io.ReadCloser) error {
	started := map[string]client.VertexStatus{}

	defer func() {
		for _, st := range started {
			st := st
			if st.Completed == nil {
				now := time.Now()
				st.Completed = &now
				l.SetStatus(&st)
			}
		}
	}()

	dec := json.NewDecoder(rc)
	var parsedErr error
	var jm jsonmessage.JSONMessage
	for {
		if err := dec.Decode(&jm); err != nil {
			if parsedErr != nil {
				return parsedErr
			}
			if err == io.EOF {
				break
			}
			return err
		}
		if jm.Error != nil {
			parsedErr = jm.Error
		}
		if jm.ID == "" || jm.Progress == nil {
			continue
		}

		id := "loading layer " + jm.ID
		st, ok := started[id]
		if !ok {
			now := time.Now()
			st = client.VertexStatus{
				ID:      id,
				Started: &now,
			}
		}
		timeDelta := time.Now().Sub(st.Timestamp)
		if timeDelta < minTimeDelta {
			continue
		}
		st.Timestamp = time.Now()
		if jm.Status == "Loading layer" {
			st.Current = jm.Progress.Current
			st.Total = jm.Progress.Total
		}
		if jm.Error != nil {
			now := time.Now()
			st.Completed = &now
		}
		started[id] = st
		l.SetStatus(&st)
	}

	return nil
}
