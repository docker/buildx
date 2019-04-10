package bake

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseCompose(t *testing.T) {
	var dt = []byte(`
version: "3"

services:
  db:
    build: ./db
    command: ./entrypoint.sh
    image: docker.io/tonistiigi/db
  webapp:
    build:
      context: ./dir
      dockerfile: Dockerfile-alternate
      args:
        buildno: 123
`)

	c, err := ParseCompose(dt)
	require.NoError(t, err)

	require.Equal(t, 1, len(c.Group))
	sort.Strings(c.Group["default"].Targets)
	require.Equal(t, []string{"db", "webapp"}, c.Group["default"].Targets)

	require.Equal(t, 2, len(c.Target))
	require.Equal(t, "./db", c.Target["db"].Context)

	require.Equal(t, "./dir", c.Target["webapp"].Context)
	require.Equal(t, "Dockerfile-alternate", c.Target["webapp"].Dockerfile)
	require.Equal(t, 1, len(c.Target["webapp"].Args))
	require.Equal(t, "123", c.Target["webapp"].Args["buildno"])
}
