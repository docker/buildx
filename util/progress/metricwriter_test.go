package progress

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCalculateIdleTime(t *testing.T) {
	for _, tt := range []struct {
		started   []int64
		completed []int64
		ms        int64
	}{
		{
			started:   []int64{0, 1, 3},
			completed: []int64{2, 10, 5},
			ms:        0,
		},
		{
			started:   []int64{0, 3},
			completed: []int64{2, 5},
			ms:        1,
		},
		{
			started:   []int64{3, 0, 7},
			completed: []int64{5, 2, 10},
			ms:        3,
		},
	} {
		started := unixMillis(tt.started...)
		completed := unixMillis(tt.completed...)

		actual := int64(calculateIdleTime(started, completed) / time.Millisecond)
		assert.Equal(t, tt.ms, actual)
	}
}

func unixMillis(ts ...int64) []time.Time {
	times := make([]time.Time, len(ts))
	for i, ms := range ts {
		times[i] = time.UnixMilli(ms)
	}
	return times
}
