package dockerutil

import (
	"io"
	"strings"
	"testing"

	"github.com/moby/buildkit/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadProgressIgnoresStaleFields(t *testing.T) {
	t.Parallel()

	logger := &captureSubLogger{}
	err := loadProgressFromReader(logger, io.NopCloser(strings.NewReader(`
{"id":"layer1","status":"Loading layer","progressDetail":{"current":5,"total":10}}
{"id":"layer2","status":"Loading layer"}
`)))
	require.NoError(t, err)

	require.Len(t, logger.statuses, 2)
	assert.Equal(t, "loading layer layer1", logger.statuses[0].ID)
	assert.Equal(t, int64(5), logger.statuses[0].Current)
	assert.Equal(t, int64(10), logger.statuses[0].Total)
	assert.Nil(t, logger.statuses[0].Completed)
	assert.Equal(t, "loading layer layer1", logger.statuses[1].ID)
	assert.NotNil(t, logger.statuses[1].Completed)
	assert.Equal(t, int64(5), logger.statuses[1].Current)
	assert.NotContains(t, statusIDs(logger.statuses), "loading layer layer2")
}

func TestPullProgressIgnoresStaleFields(t *testing.T) {
	t.Parallel()

	logger := &captureSubLogger{}
	err := PullProgressFromReader(logger, strings.NewReader(`
{"id":"layer1","status":"Downloading","progressDetail":{"current":5,"total":10}}
{"status":"Pull complete"}
{"id":"layer2","status":"Downloading"}
`))
	require.NoError(t, err)

	require.Len(t, logger.statuses, 2)
	assert.Equal(t, "pulling layer layer1", logger.statuses[0].ID)
	assert.Equal(t, int64(5), logger.statuses[0].Current)
	assert.Equal(t, int64(10), logger.statuses[0].Total)
	assert.Nil(t, logger.statuses[0].Completed)
	assert.Equal(t, "pulling layer layer1", logger.statuses[1].ID)
	assert.NotNil(t, logger.statuses[1].Completed)
	assert.Equal(t, int64(5), logger.statuses[1].Current)
	assert.NotContains(t, statusIDs(logger.statuses), "pulling layer layer2")
}

func TestPullProgressKeepsCountersOnCompletion(t *testing.T) {
	t.Parallel()

	logger := &captureSubLogger{}
	err := PullProgressFromReader(logger, strings.NewReader(`
{"id":"layer1","status":"Downloading","progressDetail":{"current":5,"total":10}}
{"id":"layer1","status":"Download complete","progressDetail":{}}
{"id":"layer1","status":"Pull complete","progressDetail":{}}
`))
	require.NoError(t, err)

	require.Len(t, logger.statuses, 3)
	assert.Equal(t, "pulling layer layer1", logger.statuses[2].ID)
	assert.Equal(t, int64(10), logger.statuses[2].Current)
	assert.Equal(t, int64(10), logger.statuses[2].Total)
	assert.NotNil(t, logger.statuses[2].Completed)
}

func TestPullProgressIgnoresPullingFrom(t *testing.T) {
	t.Parallel()

	logger := &captureSubLogger{}
	err := PullProgressFromReader(logger, strings.NewReader(`
{"id":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","status":"Pulling from library/alpine"}
{"id":"layer1","status":"Pulling fs layer"}
`))
	require.NoError(t, err)

	assert.NotContains(t, statusIDs(logger.statuses), "pulling layer sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	assert.Contains(t, statusIDs(logger.statuses), "pulling layer layer1")
}

type captureSubLogger struct {
	statuses []*client.VertexStatus
}

func (l *captureSubLogger) Wrap(name string, fn func() error) error {
	return fn()
}

func (l *captureSubLogger) Log(stream int, dt []byte) {}

func (l *captureSubLogger) SetStatus(st *client.VertexStatus) {
	cp := *st
	l.statuses = append(l.statuses, &cp)
}

func statusIDs(statuses []*client.VertexStatus) []string {
	out := make([]string, 0, len(statuses))
	for _, st := range statuses {
		out = append(out, st.ID)
	}
	return out
}
