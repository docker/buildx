package buildflags

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseContextNames(t *testing.T) {
	t.Run("normalize", func(t *testing.T) {
		contexts, err := ParseContextNames([]string{
			"library/golang=one",
			"docker.io/library/golang=two",
			"example.com/project/image:v1=three",
		})
		require.NoError(t, err)
		require.Equal(t, map[string]string{
			"golang":                       "two",
			"example.com/project/image:v1": "three",
		}, contexts)
	})

	t.Run("invalid value", func(t *testing.T) {
		_, err := ParseContextNames([]string{"golang"})
		require.EqualError(t, err, "invalid context value: golang, expected key=value")
	})

	t.Run("invalid name", func(t *testing.T) {
		_, err := ParseContextNames([]string{"UPPERCASE=value"})
		require.ErrorContains(t, err, "invalid context name UPPERCASE")
	})
}
