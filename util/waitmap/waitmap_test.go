package waitmap

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGetAfter(t *testing.T) {
	m := New()

	m.Set("foo", "bar")
	m.Set("bar", "baz")

	ctx := context.TODO()
	v, err := m.Get(ctx, "foo", "bar")
	require.NoError(t, err)

	require.Equal(t, 2, len(v))
	require.Equal(t, "bar", v["foo"])
	require.Equal(t, "baz", v["bar"])

	v, err = m.Get(ctx, "foo")
	require.NoError(t, err)
	require.Equal(t, 1, len(v))
	require.Equal(t, "bar", v["foo"])
}

func TestTimeout(t *testing.T) {
	m := New()

	m.Set("foo", "bar")

	ctx, cancel := context.WithTimeout(context.TODO(), 100*time.Millisecond)
	defer cancel()

	_, err := m.Get(ctx, "bar")
	require.Error(t, err)
	require.True(t, errors.Is(err, context.DeadlineExceeded))
}

func TestBlocking(t *testing.T) {
	m := New()

	m.Set("foo", "bar")

	go func() {
		time.Sleep(100 * time.Millisecond)
		m.Set("bar", "baz")
		time.Sleep(50 * time.Millisecond)
		m.Set("baz", "abc")
	}()

	ctx := context.TODO()
	v, err := m.Get(ctx, "foo", "bar", "baz")
	require.NoError(t, err)
	require.Equal(t, 3, len(v))
	require.Equal(t, "bar", v["foo"])
	require.Equal(t, "baz", v["bar"])
	require.Equal(t, "abc", v["baz"])
}
