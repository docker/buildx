package bake

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReadTargets(t *testing.T) {
	t.Parallel()
	tmpdir, err := ioutil.TempDir("", "bake")
	require.NoError(t, err)
	defer os.RemoveAll(tmpdir)

	fp := filepath.Join(tmpdir, "config.hcl")
	err = ioutil.WriteFile(fp, []byte(`
target "dep" {
}

target "webapp" {
	dockerfile = "Dockerfile.webapp"
	inherits = ["dep"]
}`), 0600)
	require.NoError(t, err)

	ctx := context.TODO()

	m, err := ReadTargets(ctx, []string{fp}, []string{"webapp"}, nil)
	require.NoError(t, err)

	require.Equal(t, "Dockerfile.webapp", *m["webapp"].Dockerfile)
	require.Equal(t, ".", *m["webapp"].Context)
}

func TestReadTargetsCompose(t *testing.T) {
	t.Parallel()
	tmpdir, err := ioutil.TempDir("", "bake")
	require.NoError(t, err)
	defer os.RemoveAll(tmpdir)

	fp := filepath.Join(tmpdir, "docker-compose.yml")
	err = ioutil.WriteFile(fp, []byte(`
version: "3"

services:
  db:
    build: .
    command: ./entrypoint.sh
    image: docker.io/tonistiigi/db
  webapp:
    build:
      dockerfile: Dockerfile.webapp
      args:
        buildno: 1
`), 0600)
	require.NoError(t, err)

	ctx := context.TODO()

	m, err := ReadTargets(ctx, []string{fp}, []string{"default"}, nil)
	require.NoError(t, err)

	require.Equal(t, "Dockerfile.webapp", *m["webapp"].Dockerfile)
	require.Equal(t, ".", *m["webapp"].Context)
}
