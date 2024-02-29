package imagetools

import (
	"encoding/base64"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_scanSBOM(t *testing.T) {

}

func Test_scanProvenance(t *testing.T) {

}

func Test_isInTotoDSSE(t *testing.T) {
	tests := []struct {
		mime     string
		expected bool
	}{
		{
			mime:     "application/vnd.in-toto.spdx+dsse",
			expected: true,
		},
		{
			mime:     "application/vnd.in-toto.provenance+dsse",
			expected: true,
		},
		{
			mime:     "application/vnd.in-toto+json",
			expected: false,
		},
	}

	for _, test := range tests {
		t.Run(test.mime, func(t *testing.T) {
			assert.Equal(t, isInTotoDSSE(test.mime), test.expected)
		})
	}
}

func Test_decodeDSSE(t *testing.T) {
	// Returns input when mime isn't a DSSE type
	actual, err := decodeDSSE([]byte("foobar"), "application/vnd.in-toto+json")
	assert.NoError(t, err)
	assert.Equal(t, []byte("foobar"), actual)

	// Returns the base64 decoded payload if is a DSSE
	payload := base64.StdEncoding.EncodeToString([]byte("hello world"))
	envelope := fmt.Sprintf("{\"payload\":\"%s\"}", payload)
	actual, err = decodeDSSE([]byte(envelope), "application/vnd.in-toto.spdx+dsse")
	assert.NoError(t, err)
	assert.Equal(t, "hello world", string(actual))

	actual, err = decodeDSSE([]byte("not a json"), "application/vnd.in-toto.spdx+dsse")
	assert.Error(t, err)

	actual, err = decodeDSSE([]byte("{\"payload\": \"not base64\"}"), "application/vnd.in-toto.spdx+dsse")
	assert.Error(t, err)
}
