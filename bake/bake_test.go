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

	t.Run("NoOverrides", func(t *testing.T) {
		m, err := ReadTargets(ctx, []string{fp}, []string{"webapp"}, nil)
		require.NoError(t, err)

		require.Equal(t, "Dockerfile.webapp", *m["webapp"].Dockerfile)
		require.Equal(t, ".", *m["webapp"].Context)
	})

	t.Run("ArgsOverrides", func(t *testing.T) {
		os.Setenv("VAR_FROMENV"+t.Name(), "fromEnv")
		defer os.Unsetenv("VAR_FROM_ENV" + t.Name())

		m, err := ReadTargets(ctx, []string{fp}, []string{"webapp"}, []string{
			"webapp.args.VAR_UNSET",
			"webapp.args.VAR_EMPTY=",
			"webapp.args.VAR_SET=bananas",
			"webapp.args.VAR_FROMENV" + t.Name(),
		})
		require.NoError(t, err)

		require.Equal(t, "Dockerfile.webapp", *m["webapp"].Dockerfile)
		require.Equal(t, ".", *m["webapp"].Context)

		_, isSet := m["webapp"].Args["VAR_UNSET"]
		require.False(t, isSet, m["webapp"].Args["VAR_UNSET"])

		_, isSet = m["webapp"].Args["VAR_EMPTY"]
		require.True(t, isSet, m["webapp"].Args["VAR_EMPTY"])

		require.Equal(t, m["webapp"].Args["VAR_SET"], "bananas")

		require.Equal(t, m["webapp"].Args["VAR_FROMENV"+t.Name()], "fromEnv")
	})

	t.Run("ContextOverride", func(t *testing.T) {
		_, err := ReadTargets(ctx, []string{fp}, []string{"webapp"}, []string{"webapp.context"})
		require.NotNil(t, err)

		m, err := ReadTargets(ctx, []string{fp}, []string{"webapp"}, []string{"webapp.context=foo"})
		require.NoError(t, err)

		require.Equal(t, "foo", *m["webapp"].Context)
	})

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

	fp2 := filepath.Join(tmpdir, "docker-compose2.yml")
	err = ioutil.WriteFile(fp2, []byte(`
version: "3"

services:
  newservice:
    build: .
  webapp:
    build:
      args:
        buildno2: 12
`), 0600)
	require.NoError(t, err)

	ctx := context.TODO()

	m, err := ReadTargets(ctx, []string{fp, fp2}, []string{"default"}, nil)
	require.NoError(t, err)

	require.Equal(t, 3, len(m))
	_, ok := m["newservice"]
	require.True(t, ok)
	require.Equal(t, "Dockerfile.webapp", *m["webapp"].Dockerfile)
	require.Equal(t, ".", *m["webapp"].Context)
	require.Equal(t, "1", m["webapp"].Args["buildno"])
	require.Equal(t, "12", m["webapp"].Args["buildno2"])
}
