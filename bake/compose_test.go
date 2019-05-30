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
	require.Equal(t, "./db", *c.Target["db"].Context)

	require.Equal(t, "./dir", *c.Target["webapp"].Context)
	require.Equal(t, "Dockerfile-alternate", *c.Target["webapp"].Dockerfile)
	require.Equal(t, 1, len(c.Target["webapp"].Args))
	require.Equal(t, "123", c.Target["webapp"].Args["buildno"])
}

func TestNoBuildOutOfTreeService(t *testing.T) {
	var dt = []byte(`
version: "3.7"

services:
    external:
        image: "verycooldb:1337"
    webapp:
        build: ./db
`)
	c, err := ParseCompose(dt)
	require.NoError(t, err)
	require.Equal(t, 1, len(c.Group))
}

func TestParseComposeTarget(t *testing.T) {
	var dt = []byte(`
version: "3.7"

services:
  db:
    build:
      context: ./db
      target: db
  webapp:
    build:
      context: .
      target: webapp
`)

	c, err := ParseCompose(dt)
	require.NoError(t, err)

	require.Equal(t, "db", *c.Target["db"].Target)
	require.Equal(t, "webapp", *c.Target["webapp"].Target)
}

func TestComposeBuildWithoutContext(t *testing.T) {
	var dt = []byte(`
version: "3.7"

services:
  db:
    build:
      target: db
  webapp:
    build:
      context: .
      target: webapp
`)

	c, err := ParseCompose(dt)
	require.NoError(t, err)
	require.Equal(t, "db", *c.Target["db"].Target)
	require.Equal(t, "webapp", *c.Target["webapp"].Target)
}

func TestBogusCompose(t *testing.T) {
	var dt = []byte(`
version: "3.7"

services:
  db:
    labels:
      - "foo"
  webapp:
    build:
      context: .
      target: webapp
`)

	_, err := ParseCompose(dt)
	require.Error(t, err)
	require.Contains(t, err.Error(), "has neither an image nor a build context specified. At least one must be provided")
}
