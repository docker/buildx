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

	require.Equal(t, 1, len(c.Groups))
	require.Equal(t, c.Groups[0].Name, "default")
	sort.Strings(c.Groups[0].Targets)
	require.Equal(t, []string{"db", "webapp"}, c.Groups[0].Targets)

	require.Equal(t, 2, len(c.Targets))
	sort.Slice(c.Targets, func(i, j int) bool {
		return c.Targets[i].Name < c.Targets[j].Name
	})
	require.Equal(t, "db", c.Targets[0].Name)
	require.Equal(t, "./db", *c.Targets[0].Context)

	require.Equal(t, "webapp", c.Targets[1].Name)
	require.Equal(t, "./dir", *c.Targets[1].Context)
	require.Equal(t, "Dockerfile-alternate", *c.Targets[1].Dockerfile)
	require.Equal(t, 1, len(c.Targets[1].Args))
	require.Equal(t, "123", c.Targets[1].Args["buildno"])
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
	require.Equal(t, 1, len(c.Groups))
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

	require.Equal(t, 2, len(c.Targets))
	sort.Slice(c.Targets, func(i, j int) bool {
		return c.Targets[i].Name < c.Targets[j].Name
	})
	require.Equal(t, "db", c.Targets[0].Name)
	require.Equal(t, "db", *c.Targets[0].Target)
	require.Equal(t, "webapp", c.Targets[1].Name)
	require.Equal(t, "webapp", *c.Targets[1].Target)
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
	require.Equal(t, 2, len(c.Targets))
	sort.Slice(c.Targets, func(i, j int) bool {
		return c.Targets[i].Name < c.Targets[j].Name
	})
	require.Equal(t, c.Targets[0].Name, "db")
	require.Equal(t, "db", *c.Targets[0].Target)
	require.Equal(t, c.Targets[1].Name, "webapp")
	require.Equal(t, "webapp", *c.Targets[1].Target)
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
