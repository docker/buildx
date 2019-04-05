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

	c, err := ParseHCL(dt)
	require.NoError(t, err)

	require.Equal(t, 1, len(c.Group))
	require.Equal(t, []string{"db", "webapp"}, c.Group["default"].Targets)

	require.Equal(t, 4, len(c.Target))
	require.Equal(t, "./db", c.Target["db"].Context)

	require.Equal(t, 1, len(c.Target["webapp"].Args))
	require.Equal(t, "123", c.Target["webapp"].Args["buildno"])

	require.Equal(t, 2, len(c.Target["cross"].Platforms))
	require.Equal(t, []string{"linux/amd64", "linux/arm64"}, c.Target["cross"].Platforms)
}
