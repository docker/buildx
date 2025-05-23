package bake

import (
	"fmt"
	"reflect"
	"regexp"
	"runtime"
	"testing"

	hcl "github.com/hashicorp/hcl/v2"
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
	}, nil, nil)
	require.NoError(t, err)
	require.Equal(t, 1, len(c.Targets))
	require.Equal(t, "app", c.Targets[0].Name)
	require.Equal(t, ptrstr("pre-abc"), c.Targets[0].Args["v1"])
	require.Equal(t, ptrstr("abc-post"), c.Targets[0].Args["v2"])

	t.Setenv("FOO", "def")

	c, _, err = ParseFiles([]File{
		{Data: dt, Name: "c1.hcl"},
		{Data: dt2, Name: "c2.hcl"},
	}, nil, nil)
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
	}, nil, nil)
	require.NoError(t, err)
	require.Equal(t, 1, len(c.Targets))
	require.Equal(t, "app", c.Targets[0].Name)
	require.Equal(t, ptrstr("pre--ABCDEF-"), c.Targets[0].Args["v1"])
	require.Equal(t, ptrstr("ABCDEF-post"), c.Targets[0].Args["v2"])

	t.Setenv("BASE", "new")

	c, _, err = ParseFiles([]File{
		{Data: dt, Name: "c1.hcl"},
		{Data: dt2, Name: "c2.hcl"},
	}, nil, nil)
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

func TestHCLTypedNullVariables(t *testing.T) {
	types := []string{
		"any",
		"string", "number", "bool",
		"list(string)", "set(string)", "map(string)",
		"tuple([string])", "object({val: string})",
	}
	for _, varType := range types {
		tName := fmt.Sprintf("variable typed %q with null default remains null", varType)
		t.Run(tName, func(t *testing.T) {
			dt := fmt.Sprintf(`
	            variable "FOO" {
	                type = %s
	                default = null
	            }
	
	            target "default" {
	                args = {
	                    foo = equal(FOO, null)
	                }
	            }`, varType)
			c, err := ParseFile([]byte(dt), "docker-bake.hcl")
			require.NoError(t, err)
			require.Equal(t, 1, len(c.Targets))
			require.Equal(t, "true", *c.Targets[0].Args["foo"])
		})
	}
}

func TestHCLTypedValuelessVariables(t *testing.T) {
	types := []string{
		"any",
		"string", "number", "bool",
		"list(string)", "set(string)", "map(string)",
		"tuple([string])", "object({val: string})",
	}
	for _, varType := range types {
		tName := fmt.Sprintf("variable typed %q with no default is null", varType)
		t.Run(tName, func(t *testing.T) {
			dt := fmt.Sprintf(`
                variable "FOO" {
                    type = %s
                }

                target "default" {
                    args = {
                        foo = equal(FOO, null)
                    }
                }`, varType)
			c, err := ParseFile([]byte(dt), "docker-bake.hcl")
			require.NoError(t, err)
			require.Equal(t, 1, len(c.Targets))
			require.Equal(t, "true", *c.Targets[0].Args["foo"])
		})
	}
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
			"type=sbom,disabled=true,generator=foo,\"ENV1=bar,baz\",ENV2=hello",
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
	require.Equal(t, []string{"type=provenance,mode=max", "type=sbom,disabled=true,\"ENV1=bar,baz\",ENV2=hello,generator=foo"}, stringify(c.Targets[0].Attest))
	require.Equal(t, []string{"type=local,dest=../out", "type=oci,dest=../out.tar"}, stringify(c.Targets[0].Outputs))
	require.Equal(t, []string{"type=local,src=path/to/cache", "user/app:cache"}, stringify(c.Targets[0].CacheFrom))
	require.Equal(t, []string{"type=local,dest=path/to/cache"}, stringify(c.Targets[0].CacheTo))
	require.Equal(t, []string{"id=mysecret,src=/local/secret", "id=mysecret2,env=TOKEN"}, stringify(c.Targets[0].Secrets))
	require.Equal(t, []string{"default", "key=path/to/key"}, stringify(c.Targets[0].SSH))
}

func TestHCLAttrsCapsuleType_ObjectVars(t *testing.T) {
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

func TestHCLAttrsCapsuleType_MissingVars(t *testing.T) {
	dt := []byte(`
	target "app" {
		attest = [
			"type=sbom,disabled=${SBOM}",
		]

		cache-from = [
			{ type = "registry", ref = "user/app:${FOO1}" },
      "type=local,src=path/to/cache:${FOO2}",
		]

		cache-to = [
			{ type = "local", dest = "path/to/${BAR}" },
		]

		output = [
			{ type = "oci", dest = "../${OUTPUT}.tar" },
		]

		secret = [
			{ id = "mysecret", src = "/local/${SECRET}" },
		]

		ssh = [
			{ id = "key", paths = ["path/to/${SSH_KEY}"] },
		]
	}
	`)

	var diags hcl.Diagnostics
	_, err := ParseFile(dt, "docker-bake.hcl")
	require.ErrorAs(t, err, &diags)

	re := regexp.MustCompile(`There is no variable named "([\w\d_]+)"`)
	var actual []string
	for _, diag := range diags {
		if m := re.FindStringSubmatch(diag.Error()); m != nil {
			actual = append(actual, m[1])
		}
	}
	require.ElementsMatch(t,
		[]string{"SBOM", "FOO1", "FOO2", "BAR", "OUTPUT", "SECRET", "SSH_KEY"},
		actual)
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
	}, nil, nil)
	require.NoError(t, err)
	require.Equal(t, 1, len(c.Targets))
	require.Equal(t, "app", c.Targets[0].Name)
	require.Equal(t, ptrstr("pre-def"), c.Targets[0].Args["v1"])

	t.Setenv("FOO", "ghi")

	c, _, err = ParseFiles([]File{
		{Data: dt, Name: "c1.hcl"},
		{Data: dt2, Name: "c2.hcl"},
	}, nil, nil)
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
	}, nil, nil)
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
	}, nil, nil)
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
	}, nil, map[string]string{"ABC": "11,22,33"})
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
	}, nil, nil)
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
	}, nil, map[string]string{
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
	}, nil, nil)
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
	}, nil, nil)
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

func TestEmptyVariable(t *testing.T) {
	dt := []byte(`
	variable "FOO" {}
	target "default" {
	  args = {
	    foo = equal(FOO, "")
	  }
	}`)
	c, err := ParseFile(dt, "docker-bake.hcl")
	require.NoError(t, err)
	require.Equal(t, 1, len(c.Targets))
	require.Equal(t, "true", *c.Targets[0].Args["foo"])
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

func TestVarTypingSpec(t *testing.T) {
	templ := `
        variable "FOO" {
          type = %s
        }
        target "default" {
        }`

	// not exhaustive, but the common ones
	for _, s := range []string{
		"bool", "number", "string", "any",
		"list(string)", "set(string)", "tuple([string, number])",
	} {
		dt := fmt.Sprintf(templ, s)
		_, err := ParseFile([]byte(dt), "docker-bake.hcl")
		require.NoError(t, err)
	}

	for _, s := range []string{
		"boolean",       // no synonyms/aliases
		"BOOL",          // case matters
		`lower("bool")`, // must be literals
	} {
		dt := fmt.Sprintf(templ, s)
		_, err := ParseFile([]byte(dt), "docker-bake.hcl")
		require.ErrorContains(t, err, "not a valid type")
	}
}

func TestDefaultVarTypeEnforcement(t *testing.T) {
	// To help prove a given default doesn't just pass the type check, but *is* that type,
	// we use argValue to provide an expression that would work only on that type.
	tests := []struct {
		name       string
		varType    string
		varDefault any
		argValue   string
		wantValue  string
		wantError  bool
	}{
		{
			name:       "number (happy)",
			varType:    "number",
			varDefault: 99,
			argValue:   "FOO + 1",
			wantValue:  "100",
		},
		{
			name:       "numeric string compatible with number",
			varType:    "number",
			varDefault: `"99"`,
			argValue:   "FOO + 1",
			wantValue:  "100",
		},
		{
			name:       "boolean (happy)",
			varType:    "bool",
			varDefault: true,
			argValue:   "and(FOO, true)",
			wantValue:  "true",
		},
		{
			name:       "numeric boolean compatible with boolean",
			varType:    "bool",
			varDefault: `"true"`,
			argValue:   "and(FOO, true)",
			wantValue:  "true",
		},
		// should be representative of flagrant primitive type mismatches; not worth listing all possibilities?
		{
			name:       "non-numeric string default incompatible with number",
			varType:    "number",
			varDefault: `"oops"`,
			wantError:  true,
		},
		{
			name:       "list of numbers (happy)",
			varType:    "list(number)",
			varDefault: "[2,3]",
			argValue:   `join("", [for v in FOO: v + 1])`,
			wantValue:  "34",
		},
		{
			name:       "list of numbers with numeric strings okay",
			varType:    "list(number)",
			varDefault: `["2","3"]`,
			argValue:   `join("", [for v in FOO: v + 1])`,
			wantValue:  "34",
		},
		// represent flagrant mismatches for list types
		{
			name:       "non-numeric strings in numeric list rejected",
			varType:    "list(number)",
			varDefault: `["oops"]`,
			wantError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			argValue := tt.argValue
			if argValue == "" {
				argValue = "FOO"
			}
			dt := fmt.Sprintf(`
                variable "FOO" {
                    type = %s
                    default = %v
                }

                target "default" {
                    args = {
                        foo = %s
                    }
                }`, tt.varType, tt.varDefault, argValue)
			c, err := ParseFile([]byte(dt), "docker-bake.hcl")
			if tt.wantError {
				require.ErrorContains(t, err, "invalid type")
			} else {
				require.NoError(t, err)
				if tt.wantValue != "" {
					require.Equal(t, 1, len(c.Targets))
					require.Equal(t, ptrstr(tt.wantValue), c.Targets[0].Args["foo"])
				}
			}
		})
	}
}

func TestDefaultVarTypeWithAttrValuesEnforcement(t *testing.T) {
	tests := []struct {
		name      string
		attrValue any
		varType   string
		wantError bool
	}{
		{
			name:      "attribute literal which matches var type",
			attrValue: `"hello"`,
			varType:   "string",
		},
		{
			name:      "attribute literal which coerces to var type",
			attrValue: `"99"`,
			varType:   "number",
		},
		{
			name:      "attribute from function which coerces to var type",
			attrValue: `substr("99 bottles", 0, 2)`,
			varType:   "number",
		},
		{
			name:      "attribute from function returning non-coercible value",
			attrValue: `split(",", "1,2,3foo")`,
			varType:   "list(number)",
			wantError: true,
		},
		{
			name:      "mismatch",
			attrValue: 99,
			varType:   "bool",
			wantError: true,
		},
		{
			name:      "attribute correctly typed via function",
			attrValue: `split(",", "1,2,3")`,
			varType:   "list(number)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dt := fmt.Sprintf(`
                BAR = %v
                variable "FOO" {
                    type = %s
                    default = BAR
                }

                target "default" {
                }`, tt.attrValue, tt.varType)
			_, err := ParseFile([]byte(dt), "docker-bake.hcl")
			if tt.wantError {
				require.ErrorContains(t, err, "invalid type")
				require.ErrorContains(t, err, "FOO default value")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestTypedVarOverrides(t *testing.T) {
	const unsuitableValueType = "Unsuitable value type"
	const unsupportedType = "unsupported type"
	const failedToParseElement = "failed to parse element"
	tests := []struct {
		name         string
		varType      string
		override     string
		argValue     string
		wantValue    string
		wantErrorMsg string
	}{
		{
			name:      "boolean",
			varType:   "bool",
			override:  "true",
			wantValue: "true",
		},
		{
			name:      "number",
			varType:   "number",
			override:  "99",
			wantValue: "99",
		},
		{
			name:      "unquoted string accepted",
			varType:   "string",
			override:  "hello",
			wantValue: "hello",
		},
		// an environment variable with a quoted string would most likely be intended
		// to be a string whose first and last characters are quotes
		{
			name:      "quoted string keeps quotes in value",
			varType:   "string",
			override:  `"hello"`,
			wantValue: `"hello"`,
		},
		{
			name:      "proper CSV list of strings",
			varType:   "list(string)",
			override:  "hi,there",
			argValue:  `join("-", FOO)`,
			wantValue: "hi-there",
		},
		{
			name:      "CSV of unquoted strings okay",
			varType:   "list(string)",
			override:  `hi,there`,
			argValue:  `join("-", FOO)`,
			wantValue: "hi-there",
		},
		{
			name:      "CSV list of numbers",
			varType:   "list(number)",
			override:  "3,1,4",
			argValue:  `join("-", [for v in FOO: v + 1])`,
			wantValue: "4-2-5",
		},
		{
			name:     "CSV set of numbers",
			varType:  "set(number)",
			override: "3,1,4",
			// anecdotally sets are sorted but may not be guaranteed
			argValue:  `join("-", [for v in sort(FOO): v + 1])`,
			wantValue: "2-4-5",
		},
		{
			name:      "CSV map of numbers",
			varType:   "map(number)",
			override:  "foo:1,bar:2",
			argValue:  `join("-", sort(values(FOO)))`,
			wantValue: "1-2",
		},
		{
			name:      "CSV tuple",
			varType:   "tuple([number,string])",
			override:  `99,bottles`,
			argValue:  `format("%d %s", FOO[0], FOO[1])`,
			wantValue: "99 bottles",
		},
		{
			name:         "CSV tuple elements with wrong type",
			varType:      "tuple([number,string])",
			override:     `99,100`,
			wantErrorMsg: unsuitableValueType,
		},
		{
			name:         "invalid CSV value",
			varType:      "list(string)",
			override:     `"hello,world`,
			wantErrorMsg: "from CSV",
		},
		{
			name:         "object not supported",
			varType:      "object({message: string})",
			override:     "does not matter",
			wantErrorMsg: unsupportedType,
		},
		{
			name:         "list of non-primitives not supported",
			varType:      "list(list(number))",
			override:     "1,2",
			wantErrorMsg: unsupportedType,
		},
		{
			name:         "set of non-primitives not supported",
			varType:      "set(set(number))",
			override:     "1,2",
			wantErrorMsg: unsupportedType,
		},
		{
			name:    "tuple of non-primitives not supported",
			varType: "tuple([list(number)])",
			// Intentionally a different override than other similar tests; tuple is unique in that
			// multiple types are involved and length matters.  In the real world, it's probably more
			// likely a user would accidentally omit or add an item than trying to use non-primitives,
			// so the length check comes first.
			override:     "1",
			wantErrorMsg: unsupportedType,
		},
		{
			name:         "map of non-primitives not supported",
			varType:      "map(list(number))",
			override:     "foo:1,2",
			wantErrorMsg: unsupportedType,
		},
		{
			name:    "invalid map k/v parsing",
			varType: "map(string)",
			// TODO fragile; will fail in a different manner without first k/v pair
			override:     `a:b,foo:"bar`,
			wantErrorMsg: "as CSV",
		},
		{
			name:         "list with invalidly parsed elements",
			varType:      "list(number)",
			override:     "1,1z",
			wantErrorMsg: failedToParseElement,
		},
		{
			name:         "set with invalidly parsed elements",
			varType:      "set(number)",
			override:     "1,1z",
			wantErrorMsg: failedToParseElement,
		},
		{
			name:         "tuple with invalidly parsed elements",
			varType:      "tuple([number])",
			override:     "1z",
			wantErrorMsg: failedToParseElement,
		},
		{
			name:         "map with invalidly parsed elements",
			varType:      "map(number)",
			override:     "foo:1z",
			wantErrorMsg: failedToParseElement,
		},
		{
			name:         "map with bad value format",
			varType:      "map(number)",
			override:     "foo:1:1",
			wantErrorMsg: "expected one k/v pair",
		},
		{
			name:         "primitive with bad value format",
			varType:      "number",
			override:     "1z",
			wantErrorMsg: "failed to parse",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			argValue := tt.argValue
			if argValue == "" {
				argValue = "FOO"
			}
			dt := fmt.Sprintf(`
                variable "FOO" {
                    type = %s
                }

                target "default" {
                    args = {
                        foo = %s
                    }
                }`, tt.varType, argValue)
			t.Setenv("FOO", tt.override)
			c, err := ParseFile([]byte(dt), "docker-bake.hcl")
			if tt.wantErrorMsg != "" {
				require.ErrorContains(t, err, tt.wantErrorMsg)
			} else {
				require.NoError(t, err)
				if tt.wantValue != "" {
					require.Equal(t, 1, len(c.Targets))
					require.Equal(t, tt.wantValue, *c.Targets[0].Args["foo"])
				}
			}
		})
	}
}

func TestTypedVarOverrides_JSON(t *testing.T) {
	const unsuitableValueType = "Unsuitable value type"
	tests := []struct {
		name         string
		varType      string
		override     string
		argValue     string
		wantValue    string
		wantErrorMsg string
	}{
		{
			name:      "boolean",
			varType:   "bool",
			override:  "true",
			wantValue: "true",
		},
		{
			name:      "number",
			varType:   "number",
			override:  "99",
			wantValue: "99",
		},
		// no shortcuts in JSON mode
		{
			name:         "unquoted string is error",
			varType:      "string",
			override:     "hello",
			wantErrorMsg: "from JSON",
		},
		{
			name:      "string",
			varType:   "string",
			override:  `"hello"`,
			wantValue: "hello",
		},
		{
			name:      "list of strings",
			varType:   "list(string)",
			override:  `["hi","there"]`,
			argValue:  `join("-", FOO)`,
			wantValue: "hi-there",
		},
		{
			name:      "list of numbers",
			varType:   "list(number)",
			override:  "[3, 1, 4]",
			argValue:  `join("-", [for v in FOO: v + 1])`,
			wantValue: "4-2-5",
		},
		{
			name:      "map of numbers",
			varType:   "map(number)",
			override:  `{"foo": 1, "bar": 2}`,
			argValue:  `join("-", sort(values(FOO)))`,
			wantValue: "1-2",
		},
		{
			name:     "invalid JSON map of numbers",
			varType:  "map(number)",
			override: `{"foo": "oops", "bar": 2}`,
			// in lieu of something like ErrorMatches, this is the best single phrase
			wantErrorMsg: "from JSON",
		},
		{
			name:      "tuple",
			varType:   "tuple([number,string])",
			override:  `[99, "bottles"]`,
			argValue:  `format("%d %s", FOO[0], FOO[1])`,
			wantValue: "99 bottles",
		},
		{
			name:         "tuple elements with wrong type",
			varType:      "tuple([number,string])",
			override:     `[99, 100]`,
			wantErrorMsg: unsuitableValueType,
		},
		{
			name:      "JSON object",
			varType:   `object({messages: list(string)})`,
			override:  `{"messages": ["hi", "there"]}`,
			argValue:  `join("-", FOO["messages"])`,
			wantValue: "hi-there",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			argValue := tt.argValue
			if argValue == "" {
				argValue = "FOO"
			}
			dt := fmt.Sprintf(`
                variable "FOO" {
                    type = %s
                }

                target "default" {
                    args = {
                        foo = %s
                    }
                }`, tt.varType, argValue)
			t.Setenv("FOO_JSON", tt.override)
			c, err := ParseFile([]byte(dt), "docker-bake.hcl")
			if tt.wantErrorMsg != "" {
				require.ErrorContains(t, err, tt.wantErrorMsg)
			} else {
				require.NoError(t, err)
				if tt.wantValue != "" {
					require.Equal(t, 1, len(c.Targets))
					require.Equal(t, tt.wantValue, *c.Targets[0].Args["foo"])
				}
			}
		})
	}
}

func TestJSONOverridePriority(t *testing.T) {
	t.Run("JSON override ignored when same user var exists", func(t *testing.T) {
		dt := []byte(`
            variable "FOO" {
                type = list(number)
            }
            variable "FOO_JSON" {
                type = list(number)
            }

            target "default" {
                args = {
                    foo = FOO
                }
            }`)
		// env FOO_JSON is the CSV override of var FOO_JSON, not a JSON override of FOO
		t.Setenv("FOO", "[1,2]")
		t.Setenv("FOO_JSON", "[3,4]")
		_, err := ParseFile(dt, "docker-bake.hcl")
		require.ErrorContains(t, err, "failed to convert")
		require.ErrorContains(t, err, "from CSV")
	})

	t.Run("JSON override ignored when same builtin var exists", func(t *testing.T) {
		dt := []byte(`
            variable "FOO" {
                type = list(number)
            }
            
            target "default" {
                args = {
                    foo = length(FOO)
                }
            }`)
		t.Setenv("FOO", "1,2")
		t.Setenv("FOO_JSON", "[3,4,5]")
		c, _, err := ParseFiles(
			[]File{{Name: "docker-bake.hcl", Data: dt}},
			nil,
			map[string]string{"FOO_JSON": "whatever"},
		)
		require.NoError(t, err)
		require.Equal(t, 1, len(c.Targets))
		require.Equal(t, "2", *c.Targets[0].Args["foo"])
	})

	// this is implied/exercised in other tests, but repeated for completeness
	t.Run("JSON override ignored if var is untyped", func(t *testing.T) {
		dt := []byte(`
            variable "FOO" {
                default = [1, 2]
            }
            
            target "default" {
                args = {
                    foo = length(FOO)
                }
            }`)
		t.Setenv("FOO_JSON", "[3,4]")
		_, err := ParseFile(dt, "docker-bake.hcl")
		require.ErrorContains(t, err, "unsupported type")
	})

	t.Run("override-ish variable has regular CSV override", func(t *testing.T) {
		dt := []byte(`
            variable "FOO_JSON" {
                type = list(number)
            }

            target "default" {
                args = {
                    foo = length(FOO_JSON)
                }
            }`)
		// despite the name, it's still CSV
		t.Setenv("FOO_JSON", "10,11,12")
		c, err := ParseFile(dt, "docker-bake.hcl")
		require.NoError(t, err)
		require.Equal(t, 1, len(c.Targets))
		require.Equal(t, "3", *c.Targets[0].Args["foo"])

		t.Setenv("FOO_JSON", "[10,11,12]")
		_, err = ParseFile(dt, "docker-bake.hcl")
		require.ErrorContains(t, err, "from CSV")
	})

	t.Run("override-ish variable has own JSON override", func(t *testing.T) {
		dt := []byte(`
            variable "FOO_JSON" {
                type = list(number)
            }

            target "default" {
                args = {
                    foo = length(FOO_JSON)
                }
            }`)
		t.Setenv("FOO_JSON_JSON", "[4,5,6]")
		c, err := ParseFile(dt, "docker-bake.hcl")
		require.NoError(t, err)
		require.Equal(t, 1, len(c.Targets))
		require.Equal(t, "3", *c.Targets[0].Args["foo"])
	})

	t.Run("JSON override trumps CSV when no var name conflict", func(t *testing.T) {
		dt := []byte(`
            variable "FOO" {
                type = list(number)
            }
            
            target "default" {
                args = {
                    foo = length(FOO)
                }
            }`)
		t.Setenv("FOO", "1,2")
		t.Setenv("FOO_JSON", "[3,4,5]")
		c, err := ParseFile(dt, "docker-bake.hcl")
		require.NoError(t, err)
		require.Equal(t, 1, len(c.Targets))
		require.Equal(t, "3", *c.Targets[0].Args["foo"])
	})

	t.Run("JSON override works with lowercase vars", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Windows case-insensitivity")
		}
		dt := []byte(`
            variable "foo" {
                type = number
                default = 101
            }
            
            target "default" {
                args = {
                    bar = foo
                }
            }`)
		// may seem reasonable, but not supported (on case-sensitive systems)
		t.Setenv("foo_json", "9000")
		c, err := ParseFile(dt, "docker-bake.hcl")
		require.NoError(t, err)
		require.Equal(t, 1, len(c.Targets))
		require.Equal(t, "101", *c.Targets[0].Args["bar"])

		t.Setenv("foo_JSON", "42")
		c, err = ParseFile(dt, "docker-bake.hcl")
		require.NoError(t, err)
		require.Equal(t, 1, len(c.Targets))
		require.Equal(t, "42", *c.Targets[0].Args["bar"])
	})
}

func ptrstr(s any) *string {
	var n *string
	if reflect.ValueOf(s).Kind() == reflect.String {
		ss := s.(string)
		n = &ss
	}
	return n
}
