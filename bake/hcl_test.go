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

	c, err := ParseFiles([]File{
		{Data: dt, Name: "c1.hcl"},
		{Data: dt2, Name: "c2.hcl"},
	}, nil)
	require.NoError(t, err)
	require.Equal(t, 1, len(c.Targets))
	require.Equal(t, c.Targets[0].Name, "app")
	require.Equal(t, "pre-abc", c.Targets[0].Args["v1"])
	require.Equal(t, "abc-post", c.Targets[0].Args["v2"])

	os.Setenv("FOO", "def")

	c, err = ParseFiles([]File{
		{Data: dt, Name: "c1.hcl"},
		{Data: dt2, Name: "c2.hcl"},
	}, nil)
	require.NoError(t, err)

	require.Equal(t, 1, len(c.Targets))
	require.Equal(t, c.Targets[0].Name, "app")
	require.Equal(t, "pre-def", c.Targets[0].Args["v1"])
	require.Equal(t, "def-post", c.Targets[0].Args["v2"])
}

func TestHCLVarsWithVars(t *testing.T) {
	os.Unsetenv("FOO")
	dt := []byte(`
		variable "FOO" {
			default = upper("${BASE}def")
		}
		variable "BAR" {
			default = "-${FOO}-"
		}
		target "app" {
			args = {
				v1 = "pre-${BAR}"
			}
		}
		`)
	dt2 := []byte(`
		variable "BASE" {
			default = "abc"
		}
		target "app" {
			args = {
				v2 = "${FOO}-post"
			}
		}
		`)

	c, err := ParseFiles([]File{
		{Data: dt, Name: "c1.hcl"},
		{Data: dt2, Name: "c2.hcl"},
	}, nil)
	require.NoError(t, err)
	require.Equal(t, 1, len(c.Targets))
	require.Equal(t, c.Targets[0].Name, "app")
	require.Equal(t, "pre--ABCDEF-", c.Targets[0].Args["v1"])
	require.Equal(t, "ABCDEF-post", c.Targets[0].Args["v2"])

	os.Setenv("BASE", "new")

	c, err = ParseFiles([]File{
		{Data: dt, Name: "c1.hcl"},
		{Data: dt2, Name: "c2.hcl"},
	}, nil)
	require.NoError(t, err)

	require.Equal(t, 1, len(c.Targets))
	require.Equal(t, c.Targets[0].Name, "app")
	require.Equal(t, "pre--NEWDEF-", c.Targets[0].Args["v1"])
	require.Equal(t, "NEWDEF-post", c.Targets[0].Args["v2"])
}

func TestHCLTypedVariables(t *testing.T) {
	os.Unsetenv("FOO")
	dt := []byte(`
		variable "FOO" {
			default = 3
		}
		variable "IS_FOO" {
			default = true
		}
		target "app" {
			args = {
				v1 = FOO > 5 ? "higher" : "lower" 
				v2 = IS_FOO ? "yes" : "no"
			}
		}
		`)

	c, err := ParseFile(dt, "docker-bake.hcl")
	require.NoError(t, err)

	require.Equal(t, 1, len(c.Targets))
	require.Equal(t, c.Targets[0].Name, "app")
	require.Equal(t, "lower", c.Targets[0].Args["v1"])
	require.Equal(t, "yes", c.Targets[0].Args["v2"])

	os.Setenv("FOO", "5.1")
	os.Setenv("IS_FOO", "0")

	c, err = ParseFile(dt, "docker-bake.hcl")
	require.NoError(t, err)

	require.Equal(t, 1, len(c.Targets))
	require.Equal(t, c.Targets[0].Name, "app")
	require.Equal(t, "higher", c.Targets[0].Args["v1"])
	require.Equal(t, "no", c.Targets[0].Args["v2"])

	os.Setenv("FOO", "NaN")
	_, err = ParseFile(dt, "docker-bake.hcl")
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to parse FOO as number")

	os.Setenv("FOO", "0")
	os.Setenv("IS_FOO", "maybe")

	_, err = ParseFile(dt, "docker-bake.hcl")
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to parse IS_FOO as bool")
}

func TestHCLVariableCycle(t *testing.T) {
	dt := []byte(`
		variable "FOO" {
			default = BAR
		}
		variable "FOO2" {
			default = FOO
		}
		variable "BAR" {
			default = FOO
		}
		target "app" {}
		`)

	_, err := ParseFile(dt, "docker-bake.hcl")
	require.Error(t, err)
	require.Contains(t, err.Error(), "variable cycle not allowed")
}

func TestHCLAttrs(t *testing.T) {
	dt := []byte(`
		FOO="abc"
		BAR="attr-${FOO}def"
		target "app" {
			args = {
				"v1": BAR
			}
		}
		`)

	c, err := ParseFile(dt, "docker-bake.hcl")
	require.NoError(t, err)

	require.Equal(t, 1, len(c.Targets))
	require.Equal(t, c.Targets[0].Name, "app")
	require.Equal(t, "attr-abcdef", c.Targets[0].Args["v1"])

	// env does not apply if no variable
	os.Setenv("FOO", "bar")
	c, err = ParseFile(dt, "docker-bake.hcl")
	require.NoError(t, err)

	require.Equal(t, 1, len(c.Targets))
	require.Equal(t, c.Targets[0].Name, "app")
	require.Equal(t, "attr-abcdef", c.Targets[0].Args["v1"])
	// attr-multifile
}

func TestHCLAttrsCustomType(t *testing.T) {
	dt := []byte(`
		platforms=["linux/arm64", "linux/amd64"]
		target "app" {
			platforms = platforms
			args = {
				"v1": platforms[0]
			}
		}
		`)

	c, err := ParseFile(dt, "docker-bake.hcl")
	require.NoError(t, err)

	require.Equal(t, 1, len(c.Targets))
	require.Equal(t, c.Targets[0].Name, "app")
	require.Equal(t, []string{"linux/arm64", "linux/amd64"}, c.Targets[0].Platforms)
	require.Equal(t, "linux/arm64", c.Targets[0].Args["v1"])
}

func TestHCLMultiFileAttrs(t *testing.T) {
	os.Unsetenv("FOO")
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
		FOO="def"
		`)

	c, err := ParseFiles([]File{
		{Data: dt, Name: "c1.hcl"},
		{Data: dt2, Name: "c2.hcl"},
	}, nil)
	require.NoError(t, err)
	require.Equal(t, 1, len(c.Targets))
	require.Equal(t, c.Targets[0].Name, "app")
	require.Equal(t, "pre-def", c.Targets[0].Args["v1"])

	os.Setenv("FOO", "ghi")

	c, err = ParseFiles([]File{
		{Data: dt, Name: "c1.hcl"},
		{Data: dt2, Name: "c2.hcl"},
	}, nil)
	require.NoError(t, err)

	require.Equal(t, 1, len(c.Targets))
	require.Equal(t, c.Targets[0].Name, "app")
	require.Equal(t, "pre-ghi", c.Targets[0].Args["v1"])
}

func TestJSONAttributes(t *testing.T) {
	dt := []byte(`{"FOO": "abc", "variable": {"BAR": {"default": "def"}}, "target": { "app": { "args": {"v1": "pre-${FOO}-${BAR}"}} } }`)

	c, err := ParseFile(dt, "docker-bake.json")
	require.NoError(t, err)

	require.Equal(t, 1, len(c.Targets))
	require.Equal(t, c.Targets[0].Name, "app")
	require.Equal(t, "pre-abc-def", c.Targets[0].Args["v1"])
}

func TestJSONFunctions(t *testing.T) {
	dt := []byte(`{
	"FOO": "abc",
	"function": {
		"myfunc": {
			"params": ["inp"],
			"result": "<${upper(inp)}-${FOO}>"
		}
	},
	"target": {
		"app": {
			"args": {
				"v1": "pre-${myfunc(\"foo\")}"
			}
		}
	}}`)

	c, err := ParseFile(dt, "docker-bake.json")
	require.NoError(t, err)

	require.Equal(t, 1, len(c.Targets))
	require.Equal(t, c.Targets[0].Name, "app")
	require.Equal(t, "pre-<FOO-abc>", c.Targets[0].Args["v1"])
}

func TestHCLFunctionInAttr(t *testing.T) {
	dt := []byte(`
	function "brace" {
		params = [inp]
		result = "[${inp}]"
	}
	function "myupper" {
		params = [val]
		result = "${upper(val)} <> ${brace(v2)}"
	}

		v1=myupper("foo")
		v2=lower("BAZ")
		target "app" {
			args = {
				"v1": v1
			}
		}
		`)

	c, err := ParseFile(dt, "docker-bake.hcl")
	require.NoError(t, err)

	require.Equal(t, 1, len(c.Targets))
	require.Equal(t, c.Targets[0].Name, "app")
	require.Equal(t, "FOO <> [baz]", c.Targets[0].Args["v1"])
}

func TestHCLCombineCompose(t *testing.T) {
	dt := []byte(`
		target "app" {
			context = "dir"
			args = {
				v1 = "foo"
			}
		}
		`)
	dt2 := []byte(`
version: "3"

services:
  app:
    build:
      dockerfile: Dockerfile-alternate
      args:
        v2: "bar"
`)

	c, err := ParseFiles([]File{
		{Data: dt, Name: "c1.hcl"},
		{Data: dt2, Name: "c2.yml"},
	}, nil)
	require.NoError(t, err)

	require.Equal(t, 1, len(c.Targets))
	require.Equal(t, c.Targets[0].Name, "app")
	require.Equal(t, "foo", c.Targets[0].Args["v1"])
	require.Equal(t, "bar", c.Targets[0].Args["v2"])
	require.Equal(t, "dir", *c.Targets[0].Context)
	require.Equal(t, "Dockerfile-alternate", *c.Targets[0].Dockerfile)
}

func TestHCLBuiltinVars(t *testing.T) {
	dt := []byte(`
		target "app" {
			context = BAKE_CMD_CONTEXT
			dockerfile = "test"
		}
		`)

	c, err := ParseFiles([]File{
		{Data: dt, Name: "c1.hcl"},
	}, map[string]string{
		"BAKE_CMD_CONTEXT": "foo",
	})
	require.NoError(t, err)

	require.Equal(t, 1, len(c.Targets))
	require.Equal(t, c.Targets[0].Name, "app")
	require.Equal(t, "foo", *c.Targets[0].Context)
	require.Equal(t, "test", *c.Targets[0].Dockerfile)
}
