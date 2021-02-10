package bake

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHCLBasic(t *testing.T) {
	t.Parallel()
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

	c, err := ParseFile(dt, "docker-bake.hcl")
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
}

func TestHCLBasicInJSON(t *testing.T) {
	dt := []byte(`
		{
			"group": {
				"default": {
					"targets": ["db", "webapp"]
				}
			},
			"target": {
				"db": {
					"context": "./db",
					"tags": ["docker.io/tonistiigi/db"]
				},
				"webapp": {
					"context": "./dir",
					"dockerfile": "Dockerfile-alternate",
					"args": {
						"buildno": "123"
					}
				},
				"cross": {
					"platforms": [
						"linux/amd64",
						"linux/arm64"
					]
				},
				"webapp-plus": {
					"inherits": ["webapp", "cross"],
					"args": {
						"IAMCROSS": "true"
					}
				}
			}
		}
		`)

	c, err := ParseFile(dt, "docker-bake.json")
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
}

func TestHCLWithFunctions(t *testing.T) {
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

	c, err := ParseFile(dt, "docker-bake.hcl")
	require.NoError(t, err)

	require.Equal(t, 1, len(c.Groups))
	require.Equal(t, "default", c.Groups[0].Name)
	require.Equal(t, []string{"webapp"}, c.Groups[0].Targets)

	require.Equal(t, 1, len(c.Targets))
	require.Equal(t, c.Targets[0].Name, "webapp")
	require.Equal(t, "124", c.Targets[0].Args["buildno"])
}

func TestHCLWithUserDefinedFunctions(t *testing.T) {
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

	c, err := ParseFile(dt, "docker-bake.hcl")
	require.NoError(t, err)

	require.Equal(t, 1, len(c.Groups))
	require.Equal(t, "default", c.Groups[0].Name)
	require.Equal(t, []string{"webapp"}, c.Groups[0].Targets)

	require.Equal(t, 1, len(c.Targets))
	require.Equal(t, c.Targets[0].Name, "webapp")
	require.Equal(t, "124", c.Targets[0].Args["buildno"])
}

func TestHCLWithVariables(t *testing.T) {
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

	c, err := ParseFile(dt, "docker-bake.hcl")
	require.NoError(t, err)

	require.Equal(t, 1, len(c.Groups))
	require.Equal(t, "default", c.Groups[0].Name)
	require.Equal(t, []string{"webapp"}, c.Groups[0].Targets)

	require.Equal(t, 1, len(c.Targets))
	require.Equal(t, c.Targets[0].Name, "webapp")
	require.Equal(t, "123", c.Targets[0].Args["buildno"])

	os.Setenv("BUILD_NUMBER", "456")

	c, err = ParseFile(dt, "docker-bake.hcl")
	require.NoError(t, err)

	require.Equal(t, 1, len(c.Groups))
	require.Equal(t, "default", c.Groups[0].Name)
	require.Equal(t, []string{"webapp"}, c.Groups[0].Targets)

	require.Equal(t, 1, len(c.Targets))
	require.Equal(t, c.Targets[0].Name, "webapp")
	require.Equal(t, "456", c.Targets[0].Args["buildno"])
}

func TesstHCLWithIncorrectVariables(t *testing.T) {
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

	_, err := ParseFile(dt, "docker-bake.hcl")
	require.Error(t, err)
	require.Contains(t, err.Error(), "docker-bake.hcl:7,17-37: Variables not allowed; Variables may not be used here.")
}

func TestHCLWithVariablesInFunctions(t *testing.T) {
	dt := []byte(`
		variable "REPO" {
			default = "user/repo"
		}
		function "tag" {
			params = [tag]
			result = ["${REPO}:${tag}"]
		}

		target "webapp" {
			tags = tag("v1")
		}
		`)

	c, err := ParseFile(dt, "docker-bake.hcl")
	require.NoError(t, err)

	require.Equal(t, 1, len(c.Targets))
	require.Equal(t, c.Targets[0].Name, "webapp")
	require.Equal(t, []string{"user/repo:v1"}, c.Targets[0].Tags)

	os.Setenv("REPO", "docker/buildx")

	c, err = ParseFile(dt, "docker-bake.hcl")
	require.NoError(t, err)

	require.Equal(t, 1, len(c.Targets))
	require.Equal(t, c.Targets[0].Name, "webapp")
	require.Equal(t, []string{"docker/buildx:v1"}, c.Targets[0].Tags)
}

func TestHCLMultiFileSharedVariables(t *testing.T) {
	dt := []byte(`
		variable "FOO" {
			default = "abc"
		}
		target "app" {
			args = {
				v1 = "pre-${FOO}"
			}
		}
		`)
	dt2 := []byte(`
		target "app" {
			args = {
				v2 = "${FOO}-post"
			}
		}
		`)

	c, err := parseFiles([]File{
		{Data: dt, Name: "c1.hcl"},
		{Data: dt2, Name: "c2.hcl"},
	})
	require.NoError(t, err)
	require.Equal(t, 1, len(c.Targets))
	require.Equal(t, c.Targets[0].Name, "app")
	require.Equal(t, "pre-abc", c.Targets[0].Args["v1"])
	require.Equal(t, "abc-post", c.Targets[0].Args["v2"])

	os.Setenv("FOO", "def")

	c, err = parseFiles([]File{
		{Data: dt, Name: "c1.hcl"},
		{Data: dt2, Name: "c2.hcl"},
	})
	require.NoError(t, err)

	require.Equal(t, 1, len(c.Targets))
	require.Equal(t, c.Targets[0].Name, "app")
	require.Equal(t, "pre-def", c.Targets[0].Args["v1"])
	require.Equal(t, "def-post", c.Targets[0].Args["v2"])
}
