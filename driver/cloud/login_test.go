package cloud

import (
	"bytes"
	"io"
	"net/http"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestRetryClient(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var attempts int
		transport := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			attempts++
			body, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			assert.Equal(t, "payload", string(body))

			statusCode := http.StatusServiceUnavailable
			if attempts == 3 {
				statusCode = http.StatusOK
			}
			return &http.Response{
				StatusCode: statusCode,
				Header:     make(http.Header),
				Body:       http.NoBody,
			}, nil
		})

		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://example.com", bytes.NewBufferString("payload"))
		require.NoError(t, err)

		start := time.Now()
		resp, err := newClient(transport).Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, 3, attempts)
		// The first two failed attempts wait for the initial delay and twice
		// that delay before the third attempt succeeds.
		wantWait := retryWaitInitial + 2*retryWaitInitial
		assert.Equal(t, wantWait, time.Since(start))
	})
}

func TestRetryClientRetryAfter(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var attempts int
		transport := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			attempts++
			if attempts == 1 {
				return &http.Response{
					StatusCode: http.StatusTooManyRequests,
					Header:     http.Header{"Retry-After": []string{"2"}},
					Body:       http.NoBody,
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       http.NoBody,
			}, nil
		})

		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://example.com", nil)
		require.NoError(t, err)

		start := time.Now()
		resp, err := newClient(transport).Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, 2, attempts)
		assert.Equal(t, 2*time.Second, time.Since(start))
	})
}
