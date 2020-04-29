package bake

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseHCL(t *testing.T) {
	t.Parallel()

	t.Run("Basic", func(t *testing.T) {
		dt := []byte(`
		group "default" {
			targets = ["db", "webapp"]
		}

		target "db" {
			context = "./db"
			tags = ["docker.io/tonistiigi/db"]
		}

		target "webapp" {
			context = "./dir"
			dockerfile = "Dockerfile-alternate"
			args = {
				buildno = "123"
			}
		}

		target "cross" {
			platforms = [
				"linux/amd64",
				"linux/arm64"
			]
		}

		target "webapp-plus" {
			inherits = ["webapp", "cross"]
			args = {
				IAMCROSS = "true"
			}
		}
		`)

		c, err := ParseHCL(dt, "docker-bake.hcl")
		require.NoError(t, err)

		require.Equal(t, 1, len(c.Groups))
		require.Equal(t, "default", c.Groups[0].Name)
		require.Equal(t, []string{"db", "webapp"}, c.Groups[0].Targets)

		require.Equal(t, 4, len(c.Targets))
		require.Equal(t, c.Targets[0].Name, "db")
		require.Equal(t, "./db", *c.Targets[0].Context)

		require.Equal(t, c.Targets[1].Name, "webapp")
		require.Equal(t, 1, len(c.Targets[1].Args))
		require.Equal(t, "123", c.Targets[1].Args["buildno"])

		require.Equal(t, c.Targets[2].Name, "cross")
		require.Equal(t, 2, len(c.Targets[2].Platforms))
		require.Equal(t, []string{"linux/amd64", "linux/arm64"}, c.Targets[2].Platforms)

		require.Equal(t, c.Targets[3].Name, "webapp-plus")
		require.Equal(t, 1, len(c.Targets[3].Args))
		require.Equal(t, map[string]string{"IAMCROSS": "true"}, c.Targets[3].Args)
	})

	t.Run("WithFunctions", func(t *testing.T) {
		dt := []byte(`
		group "default" {
			targets = ["webapp"]
		}

		target "webapp" {
			args = {
				buildno = "${add(123, 1)}"
			}
		}
		`)

		c, err := ParseHCL(dt, "docker-bake.hcl")
		require.NoError(t, err)

		require.Equal(t, 1, len(c.Groups))
		require.Equal(t, "default", c.Groups[0].Name)
		require.Equal(t, []string{"webapp"}, c.Groups[0].Targets)

		require.Equal(t, 1, len(c.Targets))
		require.Equal(t, c.Targets[0].Name, "webapp")
		require.Equal(t, "124", c.Targets[0].Args["buildno"])
	})

	t.Run("WithUserDefinedFunctions", func(t *testing.T) {
		dt := []byte(`
		function "increment" {
			params = [number]
			result = number + 1
		}

		group "default" {
			targets = ["webapp"]
		}

		target "webapp" {
			args = {
				buildno = "${increment(123)}"
			}
		}
		`)

		c, err := ParseHCL(dt, "docker-bake.hcl")
		require.NoError(t, err)

		require.Equal(t, 1, len(c.Groups))
		require.Equal(t, "default", c.Groups[0].Name)
		require.Equal(t, []string{"webapp"}, c.Groups[0].Targets)

		require.Equal(t, 1, len(c.Targets))
		require.Equal(t, c.Targets[0].Name, "webapp")
		require.Equal(t, "124", c.Targets[0].Args["buildno"])
	})

	t.Run("WithVariables", func(t *testing.T) {
		dt := []byte(`
		variable "BUILD_NUMBER" {
			default = "123"
		}

		group "default" {
			targets = ["webapp"]
		}

		target "webapp" {
			args = {
				buildno = "${BUILD_NUMBER}"
			}
		}
		`)

		c, err := ParseHCL(dt, "docker-bake.hcl")
		require.NoError(t, err)

		require.Equal(t, 1, len(c.Groups))
		require.Equal(t, "default", c.Groups[0].Name)
		require.Equal(t, []string{"webapp"}, c.Groups[0].Targets)

		require.Equal(t, 1, len(c.Targets))
		require.Equal(t, c.Targets[0].Name, "webapp")
		require.Equal(t, "123", c.Targets[0].Args["buildno"])

		os.Setenv("BUILD_NUMBER", "456")

		c, err = ParseHCL(dt, "docker-bake.hcl")
		require.NoError(t, err)

		require.Equal(t, 1, len(c.Groups))
		require.Equal(t, "default", c.Groups[0].Name)
		require.Equal(t, []string{"webapp"}, c.Groups[0].Targets)

		require.Equal(t, 1, len(c.Targets))
		require.Equal(t, c.Targets[0].Name, "webapp")
		require.Equal(t, "456", c.Targets[0].Args["buildno"])
	})

	t.Run("WithIncorrectVariables", func(t *testing.T) {
		dt := []byte(`
		variable "DEFAULT_BUILD_NUMBER" {
			default = "1"
		}

		variable "BUILD_NUMBER" {
			default = "${DEFAULT_BUILD_NUMBER}"
		}

		group "default" {
			targets = ["webapp"]
		}

		target "webapp" {
			args = {
				buildno = "${BUILD_NUMBER}"
			}
		}
		`)

		_, err := ParseHCL(dt, "docker-bake.hcl")
		require.Error(t, err)
		require.Contains(t, err.Error(), "docker-bake.hcl:7,17-37: Variables not allowed; Variables may not be used here.")
	})
}
