package bake

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseHCL(t *testing.T) {
	os.Setenv("BUILD_NUMBER", "456")

	var dt = []byte(`
	variable "BUILD_NUMBER" {
		default = "123"
	}

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
			buildno = "${BUILD_NUMBER}"
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
	require.Equal(t, "456", c.Targets[1].Args["buildno"])

	require.Equal(t, c.Targets[2].Name, "cross")
	require.Equal(t, 2, len(c.Targets[2].Platforms))
	require.Equal(t, []string{"linux/amd64", "linux/arm64"}, c.Targets[2].Platforms)

	require.Equal(t, c.Targets[3].Name, "webapp-plus")
	require.Equal(t, 1, len(c.Targets[3].Args))
	require.Equal(t, map[string]string{"IAMCROSS": "true"}, c.Targets[3].Args)
}
