package bake

import (
	"reflect"
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
			output = ["type=image"]
		}

		target "webapp" {
			context = "./dir"
			dockerfile = "Dockerfile-alternate"
			args = {
				buildno = "123"
			}
			output = [
				{ type = "image" }
			]
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
	require.Equal(t, "db", c.Targets[0].Name)
	require.Equal(t, "./db", *c.Targets[0].Context)

	require.Equal(t, "webapp", c.Targets[1].Name)
	require.Equal(t, 1, len(c.Targets[1].Args))
	require.Equal(t, ptrstr("123"), c.Targets[1].Args["buildno"])

	require.Equal(t, "cross", c.Targets[2].Name)
	require.Equal(t, 2, len(c.Targets[2].Platforms))
	require.Equal(t, []string{"linux/amd64", "linux/arm64"}, c.Targets[2].Platforms)

	require.Equal(t, "webapp-plus", c.Targets[3].Name)
	require.Equal(t, 1, len(c.Targets[3].Args))
	require.Equal(t, map[string]*string{"IAMCROSS": ptrstr("true")}, c.Targets[3].Args)
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
	require.Equal(t, "db", c.Targets[0].Name)
	require.Equal(t, "./db", *c.Targets[0].Context)

	require.Equal(t, "webapp", c.Targets[1].Name)
	require.Equal(t, 1, len(c.Targets[1].Args))
	require.Equal(t, ptrstr("123"), c.Targets[1].Args["buildno"])

	require.Equal(t, "cross", c.Targets[2].Name)
	require.Equal(t, 2, len(c.Targets[2].Platforms))
	require.Equal(t, []string{"linux/amd64", "linux/arm64"}, c.Targets[2].Platforms)

	require.Equal(t, "webapp-plus", c.Targets[3].Name)
	require.Equal(t, 1, len(c.Targets[3].Args))
	require.Equal(t, map[string]*string{"IAMCROSS": ptrstr("true")}, c.Targets[3].Args)
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
	require.Equal(t, "webapp", c.Targets[0].Name)
	require.Equal(t, ptrstr("124"), c.Targets[0].Args["buildno"])
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
	require.Equal(t, "webapp", c.Targets[0].Name)
	require.Equal(t, ptrstr("124"), c.Targets[0].Args["buildno"])
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
	require.Equal(t, "webapp", c.Targets[0].Name)
	require.Equal(t, ptrstr("123"), c.Targets[0].Args["buildno"])

	t.Setenv("BUILD_NUMBER", "456")

	c, err = ParseFile(dt, "docker-bake.hcl")
	require.NoError(t, err)

	require.Equal(t, 1, len(c.Groups))
	require.Equal(t, "default", c.Groups[0].Name)
	require.Equal(t, []string{"webapp"}, c.Groups[0].Targets)

	require.Equal(t, 1, len(c.Targets))
	require.Equal(t, "webapp", c.Targets[0].Name)
	require.Equal(t, ptrstr("456"), c.Targets[0].Args["buildno"])
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
	require.Equal(t, "webapp", c.Targets[0].Name)
	require.Equal(t, []string{"user/repo:v1"}, c.Targets[0].Tags)

	t.Setenv("REPO", "docker/buildx")

	c, err = ParseFile(dt, "docker-bake.hcl")
	require.NoError(t, err)

	require.Equal(t, 1, len(c.Targets))
	require.Equal(t, "webapp", c.Targets[0].Name)
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

	c, _, err := ParseFiles([]File{
		{Data: dt, Name: "c1.hcl"},
		{Data: dt2, Name: "c2.hcl"},
	}, nil)
	require.NoError(t, err)
	require.Equal(t, 1, len(c.Targets))
	require.Equal(t, "app", c.Targets[0].Name)
	require.Equal(t, ptrstr("pre-abc"), c.Targets[0].Args["v1"])
	require.Equal(t, ptrstr("abc-post"), c.Targets[0].Args["v2"])

	t.Setenv("FOO", "def")

	c, _, err = ParseFiles([]File{
		{Data: dt, Name: "c1.hcl"},
		{Data: dt2, Name: "c2.hcl"},
	}, nil)
	require.NoError(t, err)

	require.Equal(t, 1, len(c.Targets))
	require.Equal(t, "app", c.Targets[0].Name)
	require.Equal(t, ptrstr("pre-def"), c.Targets[0].Args["v1"])
	require.Equal(t, ptrstr("def-post"), c.Targets[0].Args["v2"])
}

func TestHCLVarsWithVars(t *testing.T) {
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

	c, _, err := ParseFiles([]File{
		{Data: dt, Name: "c1.hcl"},
		{Data: dt2, Name: "c2.hcl"},
	}, nil)
	require.NoError(t, err)
	require.Equal(t, 1, len(c.Targets))
	require.Equal(t, "app", c.Targets[0].Name)
	require.Equal(t, ptrstr("pre--ABCDEF-"), c.Targets[0].Args["v1"])
	require.Equal(t, ptrstr("ABCDEF-post"), c.Targets[0].Args["v2"])

	t.Setenv("BASE", "new")

	c, _, err = ParseFiles([]File{
		{Data: dt, Name: "c1.hcl"},
		{Data: dt2, Name: "c2.hcl"},
	}, nil)
	require.NoError(t, err)

	require.Equal(t, 1, len(c.Targets))
	require.Equal(t, "app", c.Targets[0].Name)
	require.Equal(t, ptrstr("pre--NEWDEF-"), c.Targets[0].Args["v1"])
	require.Equal(t, ptrstr("NEWDEF-post"), c.Targets[0].Args["v2"])
}

func TestHCLTypedVariables(t *testing.T) {
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
	require.Equal(t, "app", c.Targets[0].Name)
	require.Equal(t, ptrstr("lower"), c.Targets[0].Args["v1"])
	require.Equal(t, ptrstr("yes"), c.Targets[0].Args["v2"])

	t.Setenv("FOO", "5.1")
	t.Setenv("IS_FOO", "0")

	c, err = ParseFile(dt, "docker-bake.hcl")
	require.NoError(t, err)

	require.Equal(t, 1, len(c.Targets))
	require.Equal(t, "app", c.Targets[0].Name)
	require.Equal(t, ptrstr("higher"), c.Targets[0].Args["v1"])
	require.Equal(t, ptrstr("no"), c.Targets[0].Args["v2"])

	t.Setenv("FOO", "NaN")
	_, err = ParseFile(dt, "docker-bake.hcl")
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to parse FOO as number")

	t.Setenv("FOO", "0")
	t.Setenv("IS_FOO", "maybe")

	_, err = ParseFile(dt, "docker-bake.hcl")
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to parse IS_FOO as bool")
}

func TestHCLNullVariables(t *testing.T) {
	dt := []byte(`
		variable "FOO" {
			default = null
		}
		target "default" {
			args = {
				foo = FOO
			}
		}`)

	c, err := ParseFile(dt, "docker-bake.hcl")
	require.NoError(t, err)
	require.Equal(t, ptrstr(nil), c.Targets[0].Args["foo"])

	t.Setenv("FOO", "bar")
	c, err = ParseFile(dt, "docker-bake.hcl")
	require.NoError(t, err)
	require.Equal(t, ptrstr("bar"), c.Targets[0].Args["foo"])
}

func TestJSONNullVariables(t *testing.T) {
	dt := []byte(`{
		"variable": {
			"FOO": {
				"default": null
			}
		},
		"target": {
			"default": {
				"args": {
					"foo": "${FOO}"
				}
			}
		}
	}`)

	c, err := ParseFile(dt, "docker-bake.json")
	require.NoError(t, err)
	require.Equal(t, ptrstr(nil), c.Targets[0].Args["foo"])

	t.Setenv("FOO", "bar")
	c, err = ParseFile(dt, "docker-bake.json")
	require.NoError(t, err)
	require.Equal(t, ptrstr("bar"), c.Targets[0].Args["foo"])
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
	require.Equal(t, "app", c.Targets[0].Name)
	require.Equal(t, ptrstr("attr-abcdef"), c.Targets[0].Args["v1"])

	// env does not apply if no variable
	t.Setenv("FOO", "bar")
	c, err = ParseFile(dt, "docker-bake.hcl")
	require.NoError(t, err)

	require.Equal(t, 1, len(c.Targets))
	require.Equal(t, "app", c.Targets[0].Name)
	require.Equal(t, ptrstr("attr-abcdef"), c.Targets[0].Args["v1"])
	// attr-multifile
}

func TestHCLTargetAttrs(t *testing.T) {
	dt := []byte(`
		target "foo" {
			dockerfile = "xxx"
			context = target.bar.context
			target = target.foo.dockerfile
		}
		
		target "bar" {
			dockerfile = target.foo.dockerfile
			context = "yyy"
			target = target.bar.context
		}
		`)

	c, err := ParseFile(dt, "docker-bake.hcl")
	require.NoError(t, err)

	require.Equal(t, 2, len(c.Targets))
	require.Equal(t, "foo", c.Targets[0].Name)
	require.Equal(t, "bar", c.Targets[1].Name)

	require.Equal(t, "xxx", *c.Targets[0].Dockerfile)
	require.Equal(t, "yyy", *c.Targets[0].Context)
	require.Equal(t, "xxx", *c.Targets[0].Target)

	require.Equal(t, "xxx", *c.Targets[1].Dockerfile)
	require.Equal(t, "yyy", *c.Targets[1].Context)
	require.Equal(t, "yyy", *c.Targets[1].Target)
}

func TestHCLTargetGlobal(t *testing.T) {
	dt := []byte(`
		target "foo" {
			dockerfile = "x"
		}
		x = target.foo.dockerfile
		y = x
		target "bar" {
			dockerfile = y
		}
		`)

	c, err := ParseFile(dt, "docker-bake.hcl")
	require.NoError(t, err)

	require.Equal(t, 2, len(c.Targets))
	require.Equal(t, "foo", c.Targets[0].Name)
	require.Equal(t, "bar", c.Targets[1].Name)

	require.Equal(t, "x", *c.Targets[0].Dockerfile)
	require.Equal(t, "x", *c.Targets[1].Dockerfile)
}

func TestHCLTargetAttrName(t *testing.T) {
	dt := []byte(`
		target "foo" {
			dockerfile = target.foo.name
		}
		`)

	c, err := ParseFile(dt, "docker-bake.hcl")
	require.NoError(t, err)

	require.Equal(t, 1, len(c.Targets))
	require.Equal(t, "foo", c.Targets[0].Name)
	require.Equal(t, "foo", *c.Targets[0].Dockerfile)
}

func TestHCLTargetAttrEmptyChain(t *testing.T) {
	dt := []byte(`
		target "foo" {
			# dockerfile = Dockerfile
			context = target.foo.dockerfile
			target = target.foo.context
		}
		`)

	c, err := ParseFile(dt, "docker-bake.hcl")
	require.NoError(t, err)

	require.Equal(t, 1, len(c.Targets))
	require.Equal(t, "foo", c.Targets[0].Name)
	require.Nil(t, c.Targets[0].Dockerfile)
	require.Nil(t, c.Targets[0].Context)
	require.Nil(t, c.Targets[0].Target)
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
	require.Equal(t, "app", c.Targets[0].Name)
	require.Equal(t, []string{"linux/arm64", "linux/amd64"}, c.Targets[0].Platforms)
	require.Equal(t, ptrstr("linux/arm64"), c.Targets[0].Args["v1"])
}

func TestHCLAttrsCapsuleType(t *testing.T) {
	dt := []byte(`
	target "app" {
		attest = [
			{ type = "provenance", mode = "max" },
			"type=sbom,disabled=true",
		]

		cache-from = [
			{ type = "registry", ref = "user/app:cache" },
			"type=local,src=path/to/cache",
		]

		cache-to = [
			{ type = "local", dest = "path/to/cache" },
		]

		output = [
			{ type = "oci", dest = "../out.tar" },
			"type=local,dest=../out",
		]

		secret = [
			{ id = "mysecret", src = "/local/secret" },
			{ id = "mysecret2", env = "TOKEN" },
		]

		ssh = [
			{ id = "default" },
			{ id = "key", paths = ["path/to/key"] },
		]
	}
	`)

	c, err := ParseFile(dt, "docker-bake.hcl")
	require.NoError(t, err)

	require.Equal(t, 1, len(c.Targets))
	require.Equal(t, []string{"type=provenance,mode=max", "type=sbom,disabled=true"}, stringify(c.Targets[0].Attest))
	require.Equal(t, []string{"type=local,dest=../out", "type=oci,dest=../out.tar"}, stringify(c.Targets[0].Outputs))
	require.Equal(t, []string{"type=local,src=path/to/cache", "user/app:cache"}, stringify(c.Targets[0].CacheFrom))
	require.Equal(t, []string{"type=local,dest=path/to/cache"}, stringify(c.Targets[0].CacheTo))
	require.Equal(t, []string{"id=mysecret,src=/local/secret", "id=mysecret2,env=TOKEN"}, stringify(c.Targets[0].Secrets))
	require.Equal(t, []string{"default", "key=path/to/key"}, stringify(c.Targets[0].SSH))
}

func TestHCLAttrsCapsuleTypeVars(t *testing.T) {
	dt := []byte(`
	variable "foo" {
		default = "bar"
	}

	target "app" {
		cache-from = [
			{ type = "registry", ref = "user/app:cache" },
			"type=local,src=path/to/cache",
		]

		cache-to = [ target.app.cache-from[0] ]

		output = [
			{ type = "oci", dest = "../out.tar" },
			"type=local,dest=../out",
		]

		secret = [
			{ id = "mysecret", src = "/local/secret" },
		]

		ssh = [
			{ id = "default" },
			{ id = "key", paths = ["path/to/${target.app.output[0].type}"] },
		]
	}

	target "web" {
		cache-from = target.app.cache-from

		output = [ "type=oci,dest=../${foo}.tar" ]

		secret = [
			{ id = target.app.output[0].type, src = "/${target.app.cache-from[1].type}/secret" },
		]
	}
	`)

	c, err := ParseFile(dt, "docker-bake.hcl")
	require.NoError(t, err)

	require.Equal(t, 2, len(c.Targets))

	findTarget := func(t *testing.T, name string) *Target {
		t.Helper()
		for _, tgt := range c.Targets {
			if tgt.Name == name {
				return tgt
			}
		}
		t.Fatalf("could not find target %q", name)
		return nil
	}

	app := findTarget(t, "app")
	require.Equal(t, []string{"type=local,dest=../out", "type=oci,dest=../out.tar"}, stringify(app.Outputs))
	require.Equal(t, []string{"type=local,src=path/to/cache", "user/app:cache"}, stringify(app.CacheFrom))
	require.Equal(t, []string{"user/app:cache"}, stringify(app.CacheTo))
	require.Equal(t, []string{"id=mysecret,src=/local/secret"}, stringify(app.Secrets))
	require.Equal(t, []string{"default", "key=path/to/oci"}, stringify(app.SSH))

	web := findTarget(t, "web")
	require.Equal(t, []string{"type=oci,dest=../bar.tar"}, stringify(web.Outputs))
	require.Equal(t, []string{"type=local,src=path/to/cache", "user/app:cache"}, stringify(web.CacheFrom))
	require.Equal(t, []string{"id=oci,src=/local/secret"}, stringify(web.Secrets))
}

func TestHCLMultiFileAttrs(t *testing.T) {
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

	c, _, err := ParseFiles([]File{
		{Data: dt, Name: "c1.hcl"},
		{Data: dt2, Name: "c2.hcl"},
	}, nil)
	require.NoError(t, err)
	require.Equal(t, 1, len(c.Targets))
	require.Equal(t, "app", c.Targets[0].Name)
	require.Equal(t, ptrstr("pre-def"), c.Targets[0].Args["v1"])

	t.Setenv("FOO", "ghi")

	c, _, err = ParseFiles([]File{
		{Data: dt, Name: "c1.hcl"},
		{Data: dt2, Name: "c2.hcl"},
	}, nil)
	require.NoError(t, err)

	require.Equal(t, 1, len(c.Targets))
	require.Equal(t, "app", c.Targets[0].Name)
	require.Equal(t, ptrstr("pre-ghi"), c.Targets[0].Args["v1"])
}

func TestHCLMultiFileGlobalAttrs(t *testing.T) {
	dt := []byte(`
		FOO = "abc"
		target "app" {
			args = {
				v1 = "pre-${FOO}"
			}
		}
		`)
	dt2 := []byte(`
		FOO = "def"
		`)

	c, _, err := ParseFiles([]File{
		{Data: dt, Name: "c1.hcl"},
		{Data: dt2, Name: "c2.hcl"},
	}, nil)
	require.NoError(t, err)
	require.Equal(t, 1, len(c.Targets))
	require.Equal(t, "app", c.Targets[0].Name)
	require.Equal(t, "pre-def", *c.Targets[0].Args["v1"])
}

func TestHCLDuplicateTarget(t *testing.T) {
	dt := []byte(`
		target "app" {
			dockerfile = "x"
		}
		target "app" {
			dockerfile = "y"
		}
		`)

	c, err := ParseFile(dt, "docker-bake.hcl")
	require.NoError(t, err)

	require.Equal(t, 1, len(c.Targets))
	require.Equal(t, "app", c.Targets[0].Name)
	require.Equal(t, "y", *c.Targets[0].Dockerfile)
}

func TestHCLRenameTarget(t *testing.T) {
	dt := []byte(`
		target "abc" {
			name = "xyz"
			dockerfile = "foo"
		}
		`)

	_, err := ParseFile(dt, "docker-bake.hcl")
	require.ErrorContains(t, err, "requires matrix")
}

func TestHCLRenameGroup(t *testing.T) {
	dt := []byte(`
		group "foo" {
			name = "bar"
			targets = ["x", "y"]
		}
		`)

	_, err := ParseFile(dt, "docker-bake.hcl")
	require.ErrorContains(t, err, "not supported")

	dt = []byte(`
		group "foo" {
			matrix = {
				name = ["x", "y"]
			}
		}
		`)

	_, err = ParseFile(dt, "docker-bake.hcl")
	require.ErrorContains(t, err, "not supported")
}

func TestHCLRenameTargetAttrs(t *testing.T) {
	dt := []byte(`
		target "abc" {
			name = "xyz"
			matrix = {}
			dockerfile = "foo"
		}

		target "def" {
			dockerfile = target.xyz.dockerfile
		}
		`)

	c, err := ParseFile(dt, "docker-bake.hcl")
	require.NoError(t, err)
	require.Equal(t, 2, len(c.Targets))
	require.Equal(t, "xyz", c.Targets[0].Name)
	require.Equal(t, "foo", *c.Targets[0].Dockerfile)
	require.Equal(t, "def", c.Targets[1].Name)
	require.Equal(t, "foo", *c.Targets[1].Dockerfile)

	dt = []byte(`
		target "def" {
			dockerfile = target.xyz.dockerfile
		}

		target "abc" {
			name = "xyz"
			matrix = {}
			dockerfile = "foo"
		}
		`)

	c, err = ParseFile(dt, "docker-bake.hcl")
	require.NoError(t, err)
	require.Equal(t, 2, len(c.Targets))
	require.Equal(t, "def", c.Targets[0].Name)
	require.Equal(t, "foo", *c.Targets[0].Dockerfile)
	require.Equal(t, "xyz", c.Targets[1].Name)
	require.Equal(t, "foo", *c.Targets[1].Dockerfile)

	dt = []byte(`
		target "abc" {
			name = "xyz"
			matrix  = {}
			dockerfile = "foo"
		}

		target "def" {
			dockerfile = target.abc.dockerfile
		}
		`)

	_, err = ParseFile(dt, "docker-bake.hcl")
	require.ErrorContains(t, err, "abc")

	dt = []byte(`
		target "def" {
			dockerfile = target.abc.dockerfile
		}

		target "abc" {
			name = "xyz"
			matrix = {}
			dockerfile = "foo"
		}
		`)

	_, err = ParseFile(dt, "docker-bake.hcl")
	require.ErrorContains(t, err, "abc")
}

func TestHCLRenameSplit(t *testing.T) {
	dt := []byte(`
		target "x" {
			name = "y"
			matrix = {}
			dockerfile = "foo"
		}

		target "x" {
			name = "z"
			matrix = {}
			dockerfile = "bar"
		}
		`)

	c, err := ParseFile(dt, "docker-bake.hcl")
	require.NoError(t, err)

	require.Equal(t, 2, len(c.Targets))
	require.Equal(t, "y", c.Targets[0].Name)
	require.Equal(t, "foo", *c.Targets[0].Dockerfile)
	require.Equal(t, "z", c.Targets[1].Name)
	require.Equal(t, "bar", *c.Targets[1].Dockerfile)

	require.Equal(t, 1, len(c.Groups))
	require.Equal(t, "x", c.Groups[0].Name)
	require.Equal(t, []string{"y", "z"}, c.Groups[0].Targets)
}

func TestHCLRenameMultiFile(t *testing.T) {
	dt := []byte(`
		target "foo" {
			name = "bar"
			matrix = {}
			dockerfile = "x"
		}
		`)
	dt2 := []byte(`
		target "foo" {
			context = "y"
		}
		`)
	dt3 := []byte(`
		target "bar" {
			target = "z"
		}
		`)

	c, _, err := ParseFiles([]File{
		{Data: dt, Name: "c1.hcl"},
		{Data: dt2, Name: "c2.hcl"},
		{Data: dt3, Name: "c3.hcl"},
	}, nil)
	require.NoError(t, err)

	require.Equal(t, 2, len(c.Targets))

	require.Equal(t, "bar", c.Targets[0].Name)
	require.Equal(t, "x", *c.Targets[0].Dockerfile)
	require.Equal(t, "z", *c.Targets[0].Target)

	require.Equal(t, "foo", c.Targets[1].Name)
	require.Equal(t, "y", *c.Targets[1].Context)
}

func TestHCLMatrixBasic(t *testing.T) {
	dt := []byte(`
		target "default" {
			matrix = {
				foo = ["x", "y"]
			}
			name = foo
			dockerfile = "${foo}.Dockerfile"
		}
		`)

	c, err := ParseFile(dt, "docker-bake.hcl")
	require.NoError(t, err)

	require.Equal(t, 2, len(c.Targets))
	require.Equal(t, "x", c.Targets[0].Name)
	require.Equal(t, "y", c.Targets[1].Name)
	require.Equal(t, "x.Dockerfile", *c.Targets[0].Dockerfile)
	require.Equal(t, "y.Dockerfile", *c.Targets[1].Dockerfile)

	require.Equal(t, 1, len(c.Groups))
	require.Equal(t, "default", c.Groups[0].Name)
	require.Equal(t, []string{"x", "y"}, c.Groups[0].Targets)
}

func TestHCLMatrixMultipleKeys(t *testing.T) {
	dt := []byte(`
		target "default" {
			matrix = {
				foo = ["a"]
				bar = ["b", "c"]
				baz = ["d", "e", "f"]
			}
			name = "${foo}-${bar}-${baz}"
		}
		`)

	c, err := ParseFile(dt, "docker-bake.hcl")
	require.NoError(t, err)

	require.Equal(t, 6, len(c.Targets))
	names := make([]string, len(c.Targets))
	for i, t := range c.Targets {
		names[i] = t.Name
	}
	require.ElementsMatch(t, []string{"a-b-d", "a-b-e", "a-b-f", "a-c-d", "a-c-e", "a-c-f"}, names)

	require.Equal(t, 1, len(c.Groups))
	require.Equal(t, "default", c.Groups[0].Name)
	require.ElementsMatch(t, []string{"a-b-d", "a-b-e", "a-b-f", "a-c-d", "a-c-e", "a-c-f"}, c.Groups[0].Targets)
}

func TestHCLMatrixLists(t *testing.T) {
	dt := []byte(`
	target "foo" {
		matrix = {
			aa = [["aa", "bb"], ["cc", "dd"]]
		}
		name = aa[0]
		args = {
			target = "val${aa[1]}"
		}
	}
	`)

	c, err := ParseFile(dt, "docker-bake.hcl")
	require.NoError(t, err)

	require.Equal(t, 2, len(c.Targets))
	require.Equal(t, "aa", c.Targets[0].Name)
	require.Equal(t, ptrstr("valbb"), c.Targets[0].Args["target"])
	require.Equal(t, "cc", c.Targets[1].Name)
	require.Equal(t, ptrstr("valdd"), c.Targets[1].Args["target"])
}

func TestHCLMatrixMaps(t *testing.T) {
	dt := []byte(`
	target "foo" {
		matrix = {
			aa = [
				{
					foo = "aa"
					bar = "bb"
				},
				{
					foo = "cc"
					bar = "dd"
				}
			]
		}
		name = aa.foo
		args = {
			target = "val${aa.bar}"
		}
	}
	`)

	c, err := ParseFile(dt, "docker-bake.hcl")
	require.NoError(t, err)

	require.Equal(t, 2, len(c.Targets))
	require.Equal(t, "aa", c.Targets[0].Name)
	require.Equal(t, c.Targets[0].Args["target"], ptrstr("valbb"))
	require.Equal(t, "cc", c.Targets[1].Name)
	require.Equal(t, c.Targets[1].Args["target"], ptrstr("valdd"))
}

func TestHCLMatrixMultipleTargets(t *testing.T) {
	dt := []byte(`
		target "x" {
			matrix = {
				foo = ["a", "b"]
			}
			name = foo
		}
		target "y" {
			matrix = {
				bar = ["c", "d"]
			}
			name = bar
		}
		`)

	c, err := ParseFile(dt, "docker-bake.hcl")
	require.NoError(t, err)

	require.Equal(t, 4, len(c.Targets))
	names := make([]string, len(c.Targets))
	for i, t := range c.Targets {
		names[i] = t.Name
	}
	require.ElementsMatch(t, []string{"a", "b", "c", "d"}, names)

	require.Equal(t, 2, len(c.Groups))
	names = make([]string, len(c.Groups))
	for i, c := range c.Groups {
		names[i] = c.Name
	}
	require.ElementsMatch(t, []string{"x", "y"}, names)

	for _, g := range c.Groups {
		switch g.Name {
		case "x":
			require.Equal(t, []string{"a", "b"}, g.Targets)
		case "y":
			require.Equal(t, []string{"c", "d"}, g.Targets)
		}
	}
}

func TestHCLMatrixDuplicateNames(t *testing.T) {
	dt := []byte(`
		target "default" {
			matrix = {
				foo = ["a", "b"]
			}
			name = "c"
		}
		`)

	_, err := ParseFile(dt, "docker-bake.hcl")
	require.Error(t, err)
}

func TestHCLMatrixArgs(t *testing.T) {
	dt := []byte(`
		a = 1
		variable "b" {
			default = 2
		}
		target "default" {
			matrix = {
				foo = [a, b]
			}
			name = foo
		}
		`)

	c, err := ParseFile(dt, "docker-bake.hcl")
	require.NoError(t, err)

	require.Equal(t, 2, len(c.Targets))
	require.Equal(t, "1", c.Targets[0].Name)
	require.Equal(t, "2", c.Targets[1].Name)
}

func TestHCLMatrixArgsOverride(t *testing.T) {
	dt := []byte(`
	variable "ABC" {
		default = "def"
	}

	target "bar" {
		matrix = {
			aa = split(",", ABC)
		}
		name = "bar-${aa}"
		args = {
			foo = aa
		}
	}
	`)

	c, _, err := ParseFiles([]File{
		{Data: dt, Name: "docker-bake.hcl"},
	}, map[string]string{"ABC": "11,22,33"})
	require.NoError(t, err)

	require.Equal(t, 3, len(c.Targets))
	require.Equal(t, "bar-11", c.Targets[0].Name)
	require.Equal(t, "bar-22", c.Targets[1].Name)
	require.Equal(t, "bar-33", c.Targets[2].Name)

	require.Equal(t, ptrstr("11"), c.Targets[0].Args["foo"])
	require.Equal(t, ptrstr("22"), c.Targets[1].Args["foo"])
	require.Equal(t, ptrstr("33"), c.Targets[2].Args["foo"])
}

func TestHCLMatrixBadTypes(t *testing.T) {
	dt := []byte(`
		target "default" {
			matrix = "test"
		}
		`)
	_, err := ParseFile(dt, "docker-bake.hcl")
	require.Error(t, err)

	dt = []byte(`
		target "default" {
			matrix = ["test"]
		}
		`)
	_, err = ParseFile(dt, "docker-bake.hcl")
	require.Error(t, err)

	dt = []byte(`
		target "default" {
			matrix = {
				["a"] = ["b"]
			}
		}
		`)
	_, err = ParseFile(dt, "docker-bake.hcl")
	require.Error(t, err)

	dt = []byte(`
		target "default" {
			matrix = {
				1 = 2
			}
		}
		`)
	_, err = ParseFile(dt, "docker-bake.hcl")
	require.Error(t, err)

	dt = []byte(`
		target "default" {
			matrix = {
				a = "b"
			}
		}
		`)
	_, err = ParseFile(dt, "docker-bake.hcl")
	require.Error(t, err)
}

func TestHCLMatrixWithGlobalTarget(t *testing.T) {
	dt := []byte(`
		target "x" {
			tags = ["a", "b"]
		}
		
		target "default" {
			tags = target.x.tags
			matrix = {
				dummy = [""]
			}
		}
	`)
	c, err := ParseFile(dt, "docker-bake.hcl")
	require.NoError(t, err)
	require.Equal(t, 2, len(c.Targets))
	require.Equal(t, "x", c.Targets[0].Name)
	require.Equal(t, "default", c.Targets[1].Name)
	require.Equal(t, []string{"a", "b"}, c.Targets[1].Tags)
}

func TestJSONAttributes(t *testing.T) {
	dt := []byte(`{"FOO": "abc", "variable": {"BAR": {"default": "def"}}, "target": { "app": { "args": {"v1": "pre-${FOO}-${BAR}"}} } }`)

	c, err := ParseFile(dt, "docker-bake.json")
	require.NoError(t, err)

	require.Equal(t, 1, len(c.Targets))
	require.Equal(t, "app", c.Targets[0].Name)
	require.Equal(t, ptrstr("pre-abc-def"), c.Targets[0].Args["v1"])
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
	require.Equal(t, "app", c.Targets[0].Name)
	require.Equal(t, ptrstr("pre-<FOO-abc>"), c.Targets[0].Args["v1"])
}

func TestJSONInvalidFunctions(t *testing.T) {
	dt := []byte(`{
	"target": {
		"app": {
			"args": {
				"v1": "myfunc(\"foo\")"
			}
		}
	}}`)

	c, err := ParseFile(dt, "docker-bake.json")
	require.NoError(t, err)

	require.Equal(t, 1, len(c.Targets))
	require.Equal(t, "app", c.Targets[0].Name)
	require.Equal(t, ptrstr(`myfunc("foo")`), c.Targets[0].Args["v1"])
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
	require.Equal(t, "app", c.Targets[0].Name)
	require.Equal(t, ptrstr("FOO <> [baz]"), c.Targets[0].Args["v1"])
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

	c, _, err := ParseFiles([]File{
		{Data: dt, Name: "c1.hcl"},
		{Data: dt2, Name: "c2.yml"},
	}, nil)
	require.NoError(t, err)

	require.Equal(t, 1, len(c.Targets))
	require.Equal(t, "app", c.Targets[0].Name)
	require.Equal(t, ptrstr("foo"), c.Targets[0].Args["v1"])
	require.Equal(t, ptrstr("bar"), c.Targets[0].Args["v2"])
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

	c, _, err := ParseFiles([]File{
		{Data: dt, Name: "c1.hcl"},
	}, map[string]string{
		"BAKE_CMD_CONTEXT": "foo",
	})
	require.NoError(t, err)

	require.Equal(t, 1, len(c.Targets))
	require.Equal(t, "app", c.Targets[0].Name)
	require.Equal(t, "foo", *c.Targets[0].Context)
	require.Equal(t, "test", *c.Targets[0].Dockerfile)
}

func TestCombineHCLAndJSONTargets(t *testing.T) {
	c, _, err := ParseFiles([]File{
		{
			Name: "docker-bake.hcl",
			Data: []byte(`
group "default" {
  targets = ["a"]
}

target "metadata-a" {}
target "metadata-b" {}

target "a" {
  inherits = ["metadata-a"]
  context = "."
  target = "a"
}

target "b" {
  inherits = ["metadata-b"]
  context = "."
  target = "b"
}`),
		},
		{
			Name: "metadata-a.json",
			Data: []byte(`
{
  "target": [{
    "metadata-a": [{
      "tags": [
        "app/a:1.0.0",
        "app/a:latest"
      ]
    }]
  }]
}`),
		},
		{
			Name: "metadata-b.json",
			Data: []byte(`
{
  "target": [{
    "metadata-b": [{
      "tags": [
        "app/b:1.0.0",
        "app/b:latest"
      ]
    }]
  }]
}`),
		},
	}, nil)
	require.NoError(t, err)

	require.Equal(t, 1, len(c.Groups))
	require.Equal(t, "default", c.Groups[0].Name)
	require.Equal(t, []string{"a"}, c.Groups[0].Targets)

	require.Equal(t, 4, len(c.Targets))

	require.Equal(t, "metadata-a", c.Targets[0].Name)
	require.Equal(t, []string{"app/a:1.0.0", "app/a:latest"}, c.Targets[0].Tags)

	require.Equal(t, "metadata-b", c.Targets[1].Name)
	require.Equal(t, []string{"app/b:1.0.0", "app/b:latest"}, c.Targets[1].Tags)

	require.Equal(t, "a", c.Targets[2].Name)
	require.Equal(t, ".", *c.Targets[2].Context)
	require.Equal(t, "a", *c.Targets[2].Target)

	require.Equal(t, "b", c.Targets[3].Name)
	require.Equal(t, ".", *c.Targets[3].Context)
	require.Equal(t, "b", *c.Targets[3].Target)
}

func TestCombineHCLAndJSONVars(t *testing.T) {
	c, _, err := ParseFiles([]File{
		{
			Name: "docker-bake.hcl",
			Data: []byte(`
variable "ABC" {
  default = "foo"
}
variable "DEF" {
  default = ""
}
group "default" {
  targets = ["one"]
}
target "one" {
  args = {
    a = "pre-${ABC}"
  }
}
target "two" {
  args = {
    b = "pre-${DEF}"
  }
}`),
		},
		{
			Name: "foo.json",
			Data: []byte(`{"variable": {"DEF": {"default": "bar"}}, "target": { "one": { "args": {"a": "pre-${ABC}-${DEF}"}} } }`),
		},
		{
			Name: "bar.json",
			Data: []byte(`{"ABC": "ghi", "DEF": "jkl"}`),
		},
	}, nil)
	require.NoError(t, err)

	require.Equal(t, 1, len(c.Groups))
	require.Equal(t, "default", c.Groups[0].Name)
	require.Equal(t, []string{"one"}, c.Groups[0].Targets)

	require.Equal(t, 2, len(c.Targets))

	require.Equal(t, "one", c.Targets[0].Name)
	require.Equal(t, map[string]*string{"a": ptrstr("pre-ghi-jkl")}, c.Targets[0].Args)

	require.Equal(t, "two", c.Targets[1].Name)
	require.Equal(t, map[string]*string{"b": ptrstr("pre-jkl")}, c.Targets[1].Args)
}

func TestEmptyVariableJSON(t *testing.T) {
	dt := []byte(`{
	  "variable": {
	    "VAR": {}
	  }
	}`)
	_, err := ParseFile(dt, "docker-bake.json")
	require.NoError(t, err)
}

func TestFunctionNoParams(t *testing.T) {
	dt := []byte(`
		function "foo" {
			result = "bar"
		}
		target "foo_target" {
			args = {
				test = foo()
			}
		}
		`)

	_, err := ParseFile(dt, "docker-bake.hcl")
	require.Error(t, err)
}

func TestFunctionNoResult(t *testing.T) {
	dt := []byte(`
		function "foo" {
			params = ["a"]
		}
		`)

	_, err := ParseFile(dt, "docker-bake.hcl")
	require.Error(t, err)
}

func TestVarUnsupportedType(t *testing.T) {
	dt := []byte(`
		variable "FOO" {
			default = []
		}
		target "default" {}`)

	t.Setenv("FOO", "bar")
	_, err := ParseFile(dt, "docker-bake.hcl")
	require.Error(t, err)
}

func TestHCLIndexOfFunc(t *testing.T) {
	dt := []byte(`
		variable "APP_VERSIONS" {
		  default = [
			"1.42.4",
			"1.42.3"
		  ]
		}
		target "default" {
			args = {
				APP_VERSION = app_version
			}
			matrix = {
				app_version = APP_VERSIONS
			}
			name="app-${replace(app_version, ".", "-")}"
			tags = [
				"app:${app_version}",
				indexof(APP_VERSIONS, app_version) == 0 ? "app:latest" : "",
			]
		}
		`)

	c, err := ParseFile(dt, "docker-bake.hcl")
	require.NoError(t, err)

	require.Equal(t, 2, len(c.Targets))
	require.Equal(t, "app-1-42-4", c.Targets[0].Name)
	require.Equal(t, "app:latest", c.Targets[0].Tags[1])
	require.Equal(t, "app-1-42-3", c.Targets[1].Name)
	require.Empty(t, c.Targets[1].Tags[1])
}

func ptrstr(s interface{}) *string {
	var n *string
	if reflect.ValueOf(s).Kind() == reflect.String {
		ss := s.(string)
		n = &ss
	}
	return n
}
