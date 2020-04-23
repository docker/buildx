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
	args {
		VAR_INHERITED = "dep"
		VAR_BOTH = "dep"
	}
}

target "webapp" {
	dockerfile = "Dockerfile.webapp"
	args {
		VAR_BOTH = "webapp"
	}
	inherits = ["dep"]
}`), 0600)
	require.NoError(t, err)

	ctx := context.TODO()

	t.Run("NoOverrides", func(t *testing.T) {
		m, err := ReadTargets(ctx, []string{fp}, []string{"webapp"}, nil)
		require.NoError(t, err)
		require.Equal(t, 1, len(m))

		require.Equal(t, "Dockerfile.webapp", *m["webapp"].Dockerfile)
		require.Equal(t, ".", *m["webapp"].Context)
		require.Equal(t, "dep", m["webapp"].Args["VAR_INHERITED"])
	})

	t.Run("InvalidTargetOverrides", func(t *testing.T) {
		_, err := ReadTargets(ctx, []string{fp}, []string{"webapp"}, []string{"nosuchtarget.context=foo"})
		require.NotNil(t, err)
		require.Equal(t, err.Error(), "unknown target nosuchtarget")
	})

	t.Run("ArgsOverrides", func(t *testing.T) {
		t.Run("leaf", func(t *testing.T) {
			os.Setenv("VAR_FROMENV"+t.Name(), "fromEnv")
			defer os.Unsetenv("VAR_FROM_ENV" + t.Name())

			m, err := ReadTargets(ctx, []string{fp}, []string{"webapp"}, []string{
				"webapp.args.VAR_UNSET",
				"webapp.args.VAR_EMPTY=",
				"webapp.args.VAR_SET=bananas",
				"webapp.args.VAR_FROMENV" + t.Name(),
				"webapp.args.VAR_INHERITED=override",
				// not overriding VAR_BOTH on purpose
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

			require.Equal(t, m["webapp"].Args["VAR_BOTH"], "webapp")
			require.Equal(t, m["webapp"].Args["VAR_INHERITED"], "override")
		})

		// building leaf but overriding parent fields
		t.Run("parent", func(t *testing.T) {
			m, err := ReadTargets(ctx, []string{fp}, []string{"webapp"}, []string{
				"dep.args.VAR_INHERITED=override",
				"dep.args.VAR_BOTH=override",
			})
			require.NoError(t, err)
			require.Equal(t, m["webapp"].Args["VAR_INHERITED"], "override")
			require.Equal(t, m["webapp"].Args["VAR_BOTH"], "webapp")
		})
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
