package remoteutil

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSchemes(t *testing.T) {
	require.True(t, slices.IsSorted(schemes))
}
