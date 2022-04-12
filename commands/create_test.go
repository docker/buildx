package commands

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCsvToMap(t *testing.T) {
	d := []string{
		"\"tolerations=key=foo,value=bar;key=foo2,value=bar2\",replicas=1",
		"namespace=default",
	}
	r, err := csvToMap(d)

	require.NoError(t, err)

	require.Contains(t, r, "tolerations")
	require.Equal(t, r["tolerations"], "key=foo,value=bar;key=foo2,value=bar2")

	require.Contains(t, r, "replicas")
	require.Equal(t, r["replicas"], "1")

	require.Contains(t, r, "namespace")
	require.Equal(t, r["namespace"], "default")
}
