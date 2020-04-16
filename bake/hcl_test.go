package bake

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseHCL(t *testing.T) {
	var dt = []byte(`
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
}
