package bake

import (
	"context"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReadTargets(t *testing.T) {
	fp := File{
		Name: "config.hcl",
		Data: []byte(`
target "webDEP" {
	args = {
		VAR_INHERITED = "webDEP"
		VAR_BOTH = "webDEP"
	}
	no-cache = true
}

target "webapp" {
	dockerfile = "Dockerfile.webapp"
	args = {
		VAR_BOTH = "webapp"
	}
	inherits = ["webDEP"]
}`),
	}

	ctx := context.TODO()

	t.Run("NoOverrides", func(t *testing.T) {
		t.Parallel()
		m, g, err := ReadTargets(ctx, []File{fp}, []string{"webapp"}, nil, nil)
		require.NoError(t, err)
		require.Equal(t, 1, len(m))

		require.Equal(t, "Dockerfile.webapp", *m["webapp"].Dockerfile)
		require.Equal(t, ".", *m["webapp"].Context)
		require.Equal(t, ptrstr("webDEP"), m["webapp"].Args["VAR_INHERITED"])
		require.Equal(t, true, *m["webapp"].NoCache)
		require.Nil(t, m["webapp"].Pull)

		require.Equal(t, 1, len(g))
		require.Equal(t, []string{"webapp"}, g["default"].Targets)
	})

	t.Run("InvalidTargetOverrides", func(t *testing.T) {
		t.Parallel()
		_, _, err := ReadTargets(ctx, []File{fp}, []string{"webapp"}, []string{"nosuchtarget.context=foo"}, nil)
		require.NotNil(t, err)
		require.Equal(t, err.Error(), "could not find any target matching 'nosuchtarget'")
	})

	t.Run("ArgsOverrides", func(t *testing.T) {
		t.Run("leaf", func(t *testing.T) {
			t.Setenv("VAR_FROMENV"+t.Name(), "fromEnv")

			m, g, err := ReadTargets(ctx, []File{fp}, []string{"webapp"}, []string{
				"webapp.args.VAR_UNSET",
				"webapp.args.VAR_EMPTY=",
				"webapp.args.VAR_SET=bananas",
				"webapp.args.VAR_FROMENV" + t.Name(),
				"webapp.args.VAR_INHERITED=override",
				// not overriding VAR_BOTH on purpose
			}, nil)
			require.NoError(t, err)

			require.Equal(t, "Dockerfile.webapp", *m["webapp"].Dockerfile)
			require.Equal(t, ".", *m["webapp"].Context)

			_, isSet := m["webapp"].Args["VAR_UNSET"]
			require.False(t, isSet, m["webapp"].Args["VAR_UNSET"])

			_, isSet = m["webapp"].Args["VAR_EMPTY"]
			require.True(t, isSet, m["webapp"].Args["VAR_EMPTY"])

			require.Equal(t, ptrstr("bananas"), m["webapp"].Args["VAR_SET"])

			require.Equal(t, ptrstr("fromEnv"), m["webapp"].Args["VAR_FROMENV"+t.Name()])

			require.Equal(t, ptrstr("webapp"), m["webapp"].Args["VAR_BOTH"])
			require.Equal(t, ptrstr("override"), m["webapp"].Args["VAR_INHERITED"])

			require.Equal(t, 1, len(g))
			require.Equal(t, []string{"webapp"}, g["default"].Targets)
		})

		// building leaf but overriding parent fields
		t.Run("parent", func(t *testing.T) {
			t.Parallel()
			m, g, err := ReadTargets(ctx, []File{fp}, []string{"webapp"}, []string{
				"webDEP.args.VAR_INHERITED=override",
				"webDEP.args.VAR_BOTH=override",
			}, nil)

			require.NoError(t, err)
			require.Equal(t, ptrstr("override"), m["webapp"].Args["VAR_INHERITED"])
			require.Equal(t, ptrstr("webapp"), m["webapp"].Args["VAR_BOTH"])
			require.Equal(t, 1, len(g))
			require.Equal(t, []string{"webapp"}, g["default"].Targets)
		})
	})

	t.Run("ContextOverride", func(t *testing.T) {
		t.Parallel()
		_, _, err := ReadTargets(ctx, []File{fp}, []string{"webapp"}, []string{"webapp.context"}, nil)
		require.NotNil(t, err)

		m, g, err := ReadTargets(ctx, []File{fp}, []string{"webapp"}, []string{"webapp.context=foo"}, nil)
		require.NoError(t, err)
		require.Equal(t, "foo", *m["webapp"].Context)
		require.Equal(t, 1, len(g))
		require.Equal(t, []string{"webapp"}, g["default"].Targets)
	})

	t.Run("NoCacheOverride", func(t *testing.T) {
		t.Parallel()
		m, g, err := ReadTargets(ctx, []File{fp}, []string{"webapp"}, []string{"webapp.no-cache=false"}, nil)
		require.NoError(t, err)
		require.Equal(t, false, *m["webapp"].NoCache)
		require.Equal(t, 1, len(g))
		require.Equal(t, []string{"webapp"}, g["default"].Targets)
	})

	t.Run("PullOverride", func(t *testing.T) {
		t.Parallel()
		m, g, err := ReadTargets(ctx, []File{fp}, []string{"webapp"}, []string{"webapp.pull=false"}, nil)
		require.NoError(t, err)
		require.Equal(t, false, *m["webapp"].Pull)
		require.Equal(t, 1, len(g))
		require.Equal(t, []string{"webapp"}, g["default"].Targets)
	})

	t.Run("PatternOverride", func(t *testing.T) {
		t.Parallel()
		// same check for two cases
		multiTargetCheck := func(t *testing.T, m map[string]*Target, g map[string]*Group, err error) {
			require.NoError(t, err)
			require.Equal(t, 2, len(m))
			require.Equal(t, "foo", *m["webapp"].Dockerfile)
			require.Equal(t, ptrstr("webDEP"), m["webapp"].Args["VAR_INHERITED"])
			require.Equal(t, "foo", *m["webDEP"].Dockerfile)
			require.Equal(t, ptrstr("webDEP"), m["webDEP"].Args["VAR_INHERITED"])
			require.Equal(t, 1, len(g))
			sort.Strings(g["default"].Targets)
			require.Equal(t, []string{"webDEP", "webapp"}, g["default"].Targets)
		}

		cases := []struct {
			name      string
			targets   []string
			overrides []string
			check     func(*testing.T, map[string]*Target, map[string]*Group, error)
		}{
			{
				name:      "multi target single pattern",
				targets:   []string{"webapp", "webDEP"},
				overrides: []string{"web*.dockerfile=foo"},
				check:     multiTargetCheck,
			},
			{
				name:      "multi target multi pattern",
				targets:   []string{"webapp", "webDEP"},
				overrides: []string{"web*.dockerfile=foo", "*.args.VAR_BOTH=bar"},
				check:     multiTargetCheck,
			},
			{
				name:      "single target",
				targets:   []string{"webapp"},
				overrides: []string{"web*.dockerfile=foo"},
				check: func(t *testing.T, m map[string]*Target, g map[string]*Group, err error) {
					require.NoError(t, err)
					require.Equal(t, 1, len(m))
					require.Equal(t, "foo", *m["webapp"].Dockerfile)
					require.Equal(t, ptrstr("webDEP"), m["webapp"].Args["VAR_INHERITED"])
					require.Equal(t, 1, len(g))
					require.Equal(t, []string{"webapp"}, g["default"].Targets)
				},
			},
			{
				name:      "nomatch",
				targets:   []string{"webapp"},
				overrides: []string{"nomatch*.dockerfile=foo"},
				check: func(t *testing.T, m map[string]*Target, g map[string]*Group, err error) {
					// NOTE: I am unsure whether failing to match should always error out
					// instead of simply skipping that override.
					// Let's enforce the error and we can relax it later if users complain.
					require.NotNil(t, err)
					require.Equal(t, err.Error(), "could not find any target matching 'nomatch*'")
				},
			},
		}
		for _, test := range cases {
			t.Run(test.name, func(t *testing.T) {
				m, g, err := ReadTargets(ctx, []File{fp}, test.targets, test.overrides, nil)
				test.check(t, m, g, err)
			})
		}
	})
}

func TestPushOverride(t *testing.T) {
	t.Parallel()

	fp := File{
		Name: "docker-bake.hcl",
		Data: []byte(
			`target "app" {
				output = ["type=image,compression=zstd"]
			}`),
	}
	ctx := context.TODO()
	m, _, err := ReadTargets(ctx, []File{fp}, []string{"app"}, []string{"*.push=true"}, nil)
	require.NoError(t, err)

	require.Equal(t, 1, len(m["app"].Outputs))
	require.Equal(t, "type=image,compression=zstd,push=true", m["app"].Outputs[0])

	fp = File{
		Name: "docker-bake.hcl",
		Data: []byte(
			`target "app" {
				output = ["type=image,compression=zstd"]
			}`),
	}
	ctx = context.TODO()
	m, _, err = ReadTargets(ctx, []File{fp}, []string{"app"}, []string{"*.push=false"}, nil)
	require.NoError(t, err)

	require.Equal(t, 1, len(m["app"].Outputs))
	require.Equal(t, "type=image,compression=zstd,push=false", m["app"].Outputs[0])

	fp = File{
		Name: "docker-bake.hcl",
		Data: []byte(
			`target "app" {
			}`),
	}
	ctx = context.TODO()
	m, _, err = ReadTargets(ctx, []File{fp}, []string{"app"}, []string{"*.push=true"}, nil)
	require.NoError(t, err)

	require.Equal(t, 1, len(m["app"].Outputs))
	require.Equal(t, "type=image,push=true", m["app"].Outputs[0])
}

func TestReadTargetsCompose(t *testing.T) {
	t.Parallel()

	fp := File{
		Name: "docker-compose.yml",
		Data: []byte(
			`version: "3"
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
`),
	}

	fp2 := File{
		Name: "docker-compose2.yml",
		Data: []byte(
			`version: "3"
services:
  newservice:
    build: .
  webapp:
    build:
      args:
        buildno2: 12
`),
	}

	fp3 := File{
		Name: "docker-compose3.yml",
		Data: []byte(
			`version: "3"
services:
  webapp:
    entrypoint: echo 1
`),
	}

	ctx := context.TODO()

	cwd, err := os.Getwd()
	require.NoError(t, err)

	m, g, err := ReadTargets(ctx, []File{fp, fp2, fp3}, []string{"default"}, nil, nil)
	require.NoError(t, err)

	require.Equal(t, 3, len(m))
	_, ok := m["newservice"]

	require.True(t, ok)
	require.Equal(t, "Dockerfile.webapp", *m["webapp"].Dockerfile)
	require.Equal(t, cwd, *m["webapp"].Context)
	require.Equal(t, ptrstr("1"), m["webapp"].Args["buildno"])
	require.Equal(t, ptrstr("12"), m["webapp"].Args["buildno2"])

	require.Equal(t, 1, len(g))
	sort.Strings(g["default"].Targets)
	require.Equal(t, []string{"db", "newservice", "webapp"}, g["default"].Targets)
}

func TestReadTargetsWithDotCompose(t *testing.T) {
	t.Parallel()

	fp := File{
		Name: "docker-compose.yml",
		Data: []byte(
			`version: "3"
services:
  web.app:
    build:
      dockerfile: Dockerfile.webapp
      args:
        buildno: 1
`),
	}

	fp2 := File{
		Name: "docker-compose2.yml",
		Data: []byte(
			`version: "3"
services:
  web_app:
    build:
      args:
        buildno2: 12
`),
	}

	ctx := context.TODO()

	cwd, err := os.Getwd()
	require.NoError(t, err)

	m, _, err := ReadTargets(ctx, []File{fp}, []string{"web.app"}, nil, nil)
	require.NoError(t, err)
	require.Equal(t, 1, len(m))
	_, ok := m["web_app"]
	require.True(t, ok)
	require.Equal(t, "Dockerfile.webapp", *m["web_app"].Dockerfile)
	require.Equal(t, ptrstr("1"), m["web_app"].Args["buildno"])

	m, _, err = ReadTargets(ctx, []File{fp2}, []string{"web_app"}, nil, nil)
	require.NoError(t, err)
	require.Equal(t, 1, len(m))
	_, ok = m["web_app"]
	require.True(t, ok)
	require.Equal(t, "Dockerfile", *m["web_app"].Dockerfile)
	require.Equal(t, ptrstr("12"), m["web_app"].Args["buildno2"])

	m, g, err := ReadTargets(ctx, []File{fp, fp2}, []string{"default"}, nil, nil)
	require.NoError(t, err)
	require.Equal(t, 1, len(m))
	_, ok = m["web_app"]
	require.True(t, ok)
	require.Equal(t, "Dockerfile.webapp", *m["web_app"].Dockerfile)
	require.Equal(t, cwd, *m["web_app"].Context)
	require.Equal(t, ptrstr("1"), m["web_app"].Args["buildno"])
	require.Equal(t, ptrstr("12"), m["web_app"].Args["buildno2"])

	require.Equal(t, 1, len(g))
	sort.Strings(g["default"].Targets)
	require.Equal(t, []string{"web_app"}, g["default"].Targets)
}

func TestHCLCwdPrefix(t *testing.T) {
	fp := File{
		Name: "docker-bake.hcl",
		Data: []byte(
			`target "app" {
				context = "cwd://foo"
				dockerfile = "test"
			}`),
	}
	ctx := context.TODO()
	m, g, err := ReadTargets(ctx, []File{fp}, []string{"app"}, nil, nil)
	require.NoError(t, err)

	bo, err := TargetsToBuildOpt(m, &Input{})
	require.NoError(t, err)

	require.Equal(t, 1, len(g))
	require.Equal(t, []string{"app"}, g["default"].Targets)

	require.Equal(t, 1, len(m))
	require.Contains(t, m, "app")
	require.Equal(t, "test", *m["app"].Dockerfile)
	require.Equal(t, "foo", *m["app"].Context)

	require.Equal(t, "foo/test", bo["app"].Inputs.DockerfilePath)
	require.Equal(t, "foo", bo["app"].Inputs.ContextPath)
}

func TestOverrideMerge(t *testing.T) {
	fp := File{
		Name: "docker-bake.hcl",
		Data: []byte(
			`target "app" {
				platforms = ["linux/amd64"]
				output = ["foo"]
			}`),
	}
	ctx := context.TODO()
	m, _, err := ReadTargets(ctx, []File{fp}, []string{"app"}, []string{
		"app.platform=linux/arm",
		"app.platform=linux/ppc64le",
		"app.output=type=registry",
	}, nil)
	require.NoError(t, err)

	require.Equal(t, 1, len(m))
	_, ok := m["app"]
	require.True(t, ok)

	_, err = TargetsToBuildOpt(m, &Input{})
	require.NoError(t, err)

	require.Equal(t, []string{"linux/arm", "linux/ppc64le"}, m["app"].Platforms)
	require.Equal(t, 1, len(m["app"].Outputs))
	require.Equal(t, "type=registry", m["app"].Outputs[0])
}

func TestReadContexts(t *testing.T) {
	fp := File{
		Name: "docker-bake.hcl",
		Data: []byte(`
		target "base" {
			contexts = {
				foo: "bar"
				abc: "def"
			}
		}
		target "app" {
			inherits = ["base"]
			contexts = {
				foo: "baz"
			}
		}
		`),
	}

	ctx := context.TODO()
	m, _, err := ReadTargets(ctx, []File{fp}, []string{"app"}, []string{}, nil)
	require.NoError(t, err)

	require.Equal(t, 1, len(m))
	_, ok := m["app"]
	require.True(t, ok)

	bo, err := TargetsToBuildOpt(m, &Input{})
	require.NoError(t, err)

	ctxs := bo["app"].Inputs.NamedContexts
	require.Equal(t, 2, len(ctxs))

	require.Equal(t, "baz", ctxs["foo"].Path)
	require.Equal(t, "def", ctxs["abc"].Path)

	m, _, err = ReadTargets(ctx, []File{fp}, []string{"app"}, []string{"app.contexts.foo=bay", "base.contexts.ghi=jkl"}, nil)
	require.NoError(t, err)

	require.Equal(t, 1, len(m))
	_, ok = m["app"]
	require.True(t, ok)

	bo, err = TargetsToBuildOpt(m, &Input{})
	require.NoError(t, err)

	ctxs = bo["app"].Inputs.NamedContexts
	require.Equal(t, 3, len(ctxs))

	require.Equal(t, "bay", ctxs["foo"].Path)
	require.Equal(t, "def", ctxs["abc"].Path)
	require.Equal(t, "jkl", ctxs["ghi"].Path)

	// test resetting base values
	m, _, err = ReadTargets(ctx, []File{fp}, []string{"app"}, []string{"app.contexts.foo="}, nil)
	require.NoError(t, err)

	require.Equal(t, 1, len(m))
	_, ok = m["app"]
	require.True(t, ok)

	bo, err = TargetsToBuildOpt(m, &Input{})
	require.NoError(t, err)

	ctxs = bo["app"].Inputs.NamedContexts
	require.Equal(t, 1, len(ctxs))
	require.Equal(t, "def", ctxs["abc"].Path)
}

func TestReadContextFromTargetUnknown(t *testing.T) {
	fp := File{
		Name: "docker-bake.hcl",
		Data: []byte(`
		target "base" {
			contexts = {
				foo: "bar"
				abc: "def"
			}
		}
		target "app" {
			contexts = {
				foo: "baz"
				bar: "target:bar"
			}
		}
		`),
	}

	ctx := context.TODO()
	_, _, err := ReadTargets(ctx, []File{fp}, []string{"app"}, []string{}, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to find target bar")
}

func TestReadEmptyTargets(t *testing.T) {
	t.Parallel()

	fp := File{
		Name: "docker-bake.hcl",
		Data: []byte(`target "app1" {}`),
	}

	fp2 := File{
		Name: "docker-compose.yml",
		Data: []byte(`
services:
  app2:
    build: {}
`),
	}

	ctx := context.TODO()

	cwd, err := os.Getwd()
	require.NoError(t, err)

	m, _, err := ReadTargets(ctx, []File{fp, fp2}, []string{"app1", "app2"}, nil, nil)
	require.NoError(t, err)

	require.Equal(t, 2, len(m))
	_, ok := m["app1"]
	require.True(t, ok)
	_, ok = m["app2"]
	require.True(t, ok)

	require.Equal(t, "Dockerfile", *m["app1"].Dockerfile)
	require.Equal(t, ".", *m["app1"].Context)
	require.Equal(t, "Dockerfile", *m["app2"].Dockerfile)
	require.Equal(t, cwd, *m["app2"].Context)
}

func TestReadContextFromTargetChain(t *testing.T) {
	ctx := context.TODO()
	fp := File{
		Name: "docker-bake.hcl",
		Data: []byte(`
		target "base" {
		}
		target "mid" {
			output = ["foo"]
			contexts = {
				parent: "target:base"
			}
		}
		target "app" {
			contexts = {
				foo: "baz"
				bar: "target:mid"
			}
		}
		target "unused" {}
		`),
	}

	m, _, err := ReadTargets(ctx, []File{fp}, []string{"app"}, []string{}, nil)
	require.NoError(t, err)

	require.Equal(t, 3, len(m))
	app, ok := m["app"]
	require.True(t, ok)

	require.Equal(t, 2, len(app.Contexts))

	mid, ok := m["mid"]
	require.True(t, ok)
	require.Equal(t, 0, len(mid.Outputs))
	require.Equal(t, 1, len(mid.Contexts))

	base, ok := m["base"]
	require.True(t, ok)
	require.Equal(t, 0, len(base.Contexts))
}

func TestReadContextFromTargetInfiniteLoop(t *testing.T) {
	ctx := context.TODO()
	fp := File{
		Name: "docker-bake.hcl",
		Data: []byte(`
		target "mid" {
			output = ["foo"]
			contexts = {
				parent: "target:app"
			}
		}
		target "app" {
			contexts = {
				foo: "baz"
				bar: "target:mid"
			}
		}
		`),
	}
	_, _, err := ReadTargets(ctx, []File{fp}, []string{"app", "mid"}, []string{}, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "infinite loop from")
}

func TestReadContextFromTargetMultiPlatform(t *testing.T) {
	ctx := context.TODO()
	fp := File{
		Name: "docker-bake.hcl",
		Data: []byte(`
		target "mid" {
			output = ["foo"]
			platforms = ["linux/amd64", "linux/arm64"]
		}
		target "app" {
			contexts = {
				bar: "target:mid"
			}
			platforms = ["linux/amd64", "linux/arm64"]
		}
		`),
	}
	_, _, err := ReadTargets(ctx, []File{fp}, []string{"app"}, []string{}, nil)
	require.NoError(t, err)
}

func TestReadContextFromTargetInvalidPlatforms(t *testing.T) {
	ctx := context.TODO()
	fp := File{
		Name: "docker-bake.hcl",
		Data: []byte(`
		target "mid" {
			output = ["foo"]
			platforms = ["linux/amd64", "linux/riscv64"]
		}
		target "app" {
			contexts = {
				bar: "target:mid"
			}
			platforms = ["linux/amd64", "linux/arm64"]
		}
		`),
	}
	_, _, err := ReadTargets(ctx, []File{fp}, []string{"app"}, []string{}, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "defined for different platforms")
}

func TestReadTargetsDefault(t *testing.T) {
	t.Parallel()
	ctx := context.TODO()

	f := File{
		Name: "docker-bake.hcl",
		Data: []byte(`
target "default" {
  dockerfile = "test"
}`)}

	m, g, err := ReadTargets(ctx, []File{f}, []string{"default"}, nil, nil)
	require.NoError(t, err)
	require.Equal(t, 0, len(g))
	require.Equal(t, 1, len(m))
	require.Equal(t, "test", *m["default"].Dockerfile)
}

func TestReadTargetsSpecified(t *testing.T) {
	t.Parallel()
	ctx := context.TODO()

	f := File{
		Name: "docker-bake.hcl",
		Data: []byte(`
target "image" {
  dockerfile = "test"
}`)}

	_, _, err := ReadTargets(ctx, []File{f}, []string{"default"}, nil, nil)
	require.Error(t, err)

	m, g, err := ReadTargets(ctx, []File{f}, []string{"image"}, nil, nil)
	require.NoError(t, err)
	require.Equal(t, 1, len(g))
	require.Equal(t, []string{"image"}, g["default"].Targets)
	require.Equal(t, 1, len(m))
	require.Equal(t, "test", *m["image"].Dockerfile)
}

func TestReadTargetsGroup(t *testing.T) {
	t.Parallel()
	ctx := context.TODO()

	f := File{
		Name: "docker-bake.hcl",
		Data: []byte(`
group "foo" {
  targets = ["image"]
}
target "image" {
  dockerfile = "test"
}`)}

	m, g, err := ReadTargets(ctx, []File{f}, []string{"foo"}, nil, nil)
	require.NoError(t, err)
	require.Equal(t, 2, len(g))
	require.Equal(t, []string{"foo"}, g["default"].Targets)
	require.Equal(t, []string{"image"}, g["foo"].Targets)
	require.Equal(t, 1, len(m))
	require.Equal(t, "test", *m["image"].Dockerfile)
}

func TestReadTargetsGroupAndTarget(t *testing.T) {
	t.Parallel()
	ctx := context.TODO()

	f := File{
		Name: "docker-bake.hcl",
		Data: []byte(`
group "foo" {
  targets = ["image"]
}
target "foo" {
  dockerfile = "bar"
}
target "image" {
  dockerfile = "test"
}`)}

	m, g, err := ReadTargets(ctx, []File{f}, []string{"foo"}, nil, nil)
	require.NoError(t, err)
	require.Equal(t, 2, len(g))
	require.Equal(t, []string{"foo"}, g["default"].Targets)
	require.Equal(t, []string{"image"}, g["foo"].Targets)
	require.Equal(t, 1, len(m))
	require.Equal(t, "test", *m["image"].Dockerfile)

	m, g, err = ReadTargets(ctx, []File{f}, []string{"foo", "foo"}, nil, nil)
	require.NoError(t, err)
	require.Equal(t, 2, len(g))
	require.Equal(t, []string{"foo"}, g["default"].Targets)
	require.Equal(t, []string{"image"}, g["foo"].Targets)
	require.Equal(t, 1, len(m))
	require.Equal(t, "test", *m["image"].Dockerfile)
}

func TestReadTargetsMixed(t *testing.T) {
	t.Parallel()
	ctx := context.TODO()

	fhcl := File{
		Name: "docker-bake.hcl",
		Data: []byte(`
group "default" {
  targets = ["image"]
}
target "nocache" {
  no-cache = true
}
group "release" {
  targets = ["image-release"]
}
target "image" {
  inherits = ["nocache"]
  output = ["type=docker"]
}
target "image-release" {
  inherits = ["image"]
  output = ["type=image,push=true"]
  tags = ["user/app:latest"]
}`)}

	fyml := File{
		Name: "docker-compose.yml",
		Data: []byte(`
services:
  addon:
    build:
      context: .
      dockerfile: ./Dockerfile
      args:
        CT_ECR: foo
        CT_TAG: bar
    image: ct-addon:bar
    environment:
      - NODE_ENV=test
      - AWS_ACCESS_KEY_ID=dummy
      - AWS_SECRET_ACCESS_KEY=dummy
  aws:
    build:
      dockerfile: ./aws.Dockerfile
      args:
        CT_ECR: foo
        CT_TAG: bar
    image: ct-fake-aws:bar`)}

	fjson := File{
		Name: "docker-bake.json",
		Data: []byte(`{
	 "group": {
	   "default": {
	     "targets": [
	       "image"
	     ]
	   }
	 },
	 "target": {
	   "image": {
	     "context": ".",
	     "dockerfile": "Dockerfile",
	     "output": [
	       "type=docker"
	     ]
	   }
	 }
	}`)}

	m, g, err := ReadTargets(ctx, []File{fhcl}, []string{"default"}, nil, nil)
	require.NoError(t, err)
	require.Equal(t, 1, len(g))
	require.Equal(t, []string{"image"}, g["default"].Targets)
	require.Equal(t, 1, len(m))
	require.Equal(t, 1, len(m["image"].Outputs))
	require.Equal(t, "type=docker", m["image"].Outputs[0])

	m, g, err = ReadTargets(ctx, []File{fhcl}, []string{"image-release"}, nil, nil)
	require.NoError(t, err)
	require.Equal(t, 1, len(g))
	require.Equal(t, []string{"image-release"}, g["default"].Targets)
	require.Equal(t, 1, len(m))
	require.Equal(t, 1, len(m["image-release"].Outputs))
	require.Equal(t, "type=image,push=true", m["image-release"].Outputs[0])

	m, g, err = ReadTargets(ctx, []File{fhcl}, []string{"image", "image-release"}, nil, nil)
	require.NoError(t, err)
	require.Equal(t, 1, len(g))
	require.Equal(t, []string{"image", "image-release"}, g["default"].Targets)
	require.Equal(t, 2, len(m))
	require.Equal(t, ".", *m["image"].Context)
	require.Equal(t, 1, len(m["image-release"].Outputs))
	require.Equal(t, "type=image,push=true", m["image-release"].Outputs[0])

	m, g, err = ReadTargets(ctx, []File{fyml, fhcl}, []string{"default"}, nil, nil)
	require.NoError(t, err)
	require.Equal(t, 1, len(g))
	require.Equal(t, []string{"image"}, g["default"].Targets)
	require.Equal(t, 1, len(m))
	require.Equal(t, ".", *m["image"].Context)

	m, g, err = ReadTargets(ctx, []File{fjson}, []string{"default"}, nil, nil)
	require.NoError(t, err)
	require.Equal(t, 1, len(g))
	require.Equal(t, []string{"image"}, g["default"].Targets)
	require.Equal(t, 1, len(m))
	require.Equal(t, ".", *m["image"].Context)

	m, g, err = ReadTargets(ctx, []File{fyml}, []string{"default"}, nil, nil)
	require.NoError(t, err)
	require.Equal(t, 1, len(g))
	sort.Strings(g["default"].Targets)
	require.Equal(t, []string{"addon", "aws"}, g["default"].Targets)
	require.Equal(t, 2, len(m))
	require.Equal(t, "./Dockerfile", *m["addon"].Dockerfile)
	require.Equal(t, "./aws.Dockerfile", *m["aws"].Dockerfile)

	m, g, err = ReadTargets(ctx, []File{fyml, fhcl}, []string{"addon", "aws"}, nil, nil)
	require.NoError(t, err)
	require.Equal(t, 1, len(g))
	sort.Strings(g["default"].Targets)
	require.Equal(t, []string{"addon", "aws"}, g["default"].Targets)
	require.Equal(t, 2, len(m))
	require.Equal(t, "./Dockerfile", *m["addon"].Dockerfile)
	require.Equal(t, "./aws.Dockerfile", *m["aws"].Dockerfile)

	m, g, err = ReadTargets(ctx, []File{fyml, fhcl}, []string{"addon", "aws", "image"}, nil, nil)
	require.NoError(t, err)
	require.Equal(t, 1, len(g))
	sort.Strings(g["default"].Targets)
	require.Equal(t, []string{"addon", "aws", "image"}, g["default"].Targets)
	require.Equal(t, 3, len(m))
	require.Equal(t, ".", *m["image"].Context)
	require.Equal(t, "./Dockerfile", *m["addon"].Dockerfile)
	require.Equal(t, "./aws.Dockerfile", *m["aws"].Dockerfile)
}

func TestReadTargetsSameGroupTarget(t *testing.T) {
	t.Parallel()
	ctx := context.TODO()

	f := File{
		Name: "docker-bake.hcl",
		Data: []byte(`
group "foo" {
  targets = ["foo"]
}
target "foo" {
  dockerfile = "bar"
}
target "image" {
  output = ["type=docker"]
}`)}

	m, g, err := ReadTargets(ctx, []File{f}, []string{"foo"}, nil, nil)
	require.NoError(t, err)
	require.Equal(t, 2, len(g))
	require.Equal(t, []string{"foo"}, g["default"].Targets)
	require.Equal(t, []string{"foo"}, g["foo"].Targets)
	require.Equal(t, 1, len(m))
	require.Equal(t, "bar", *m["foo"].Dockerfile)

	m, g, err = ReadTargets(ctx, []File{f}, []string{"foo", "foo"}, nil, nil)
	require.NoError(t, err)
	require.Equal(t, 2, len(g))
	require.Equal(t, []string{"foo"}, g["default"].Targets)
	require.Equal(t, []string{"foo"}, g["foo"].Targets)
	require.Equal(t, 1, len(m))
	require.Equal(t, "bar", *m["foo"].Dockerfile)
}

func TestReadTargetsSameGroupTargetMulti(t *testing.T) {
	t.Parallel()
	ctx := context.TODO()

	f := File{
		Name: "docker-bake.hcl",
		Data: []byte(`
group "foo" {
  targets = ["foo", "image"]
}
target "foo" {
  dockerfile = "bar"
}
target "image" {
  output = ["type=docker"]
}`)}

	m, g, err := ReadTargets(ctx, []File{f}, []string{"foo"}, nil, nil)
	require.NoError(t, err)
	require.Equal(t, 2, len(g))
	require.Equal(t, []string{"foo"}, g["default"].Targets)
	require.Equal(t, []string{"foo", "image"}, g["foo"].Targets)
	require.Equal(t, 2, len(m))
	require.Equal(t, "bar", *m["foo"].Dockerfile)
	require.Equal(t, "type=docker", m["image"].Outputs[0])

	m, g, err = ReadTargets(ctx, []File{f}, []string{"foo", "image"}, nil, nil)
	require.NoError(t, err)
	require.Equal(t, 2, len(g))
	require.Equal(t, []string{"foo", "image"}, g["default"].Targets)
	require.Equal(t, []string{"foo", "image"}, g["foo"].Targets)
	require.Equal(t, 2, len(m))
	require.Equal(t, "bar", *m["foo"].Dockerfile)
	require.Equal(t, "type=docker", m["image"].Outputs[0])
}

func TestNestedInherits(t *testing.T) {
	ctx := context.TODO()

	f := File{
		Name: "docker-bake.hcl",
		Data: []byte(`
target "a" {
  args = {
    foo = "123"
    bar = "234"
  }
}
target "b" {
  inherits = ["a"]
  args = {
    bar = "567"
  }
}
target "c" {
  inherits = ["a"]
  args = {
    baz = "890"
  }
}
target "d" {
  inherits = ["b", "c"]
}`)}

	cases := []struct {
		name      string
		overrides []string
		want      map[string]*string
	}{
		{
			name:      "nested simple",
			overrides: nil,
			want:      map[string]*string{"bar": ptrstr("234"), "baz": ptrstr("890"), "foo": ptrstr("123")},
		},
		{
			name:      "nested with overrides first",
			overrides: []string{"a.args.foo=321", "b.args.bar=432"},
			want:      map[string]*string{"bar": ptrstr("234"), "baz": ptrstr("890"), "foo": ptrstr("321")},
		},
		{
			name:      "nested with overrides last",
			overrides: []string{"a.args.foo=321", "c.args.bar=432"},
			want:      map[string]*string{"bar": ptrstr("432"), "baz": ptrstr("890"), "foo": ptrstr("321")},
		},
	}
	for _, tt := range cases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			m, g, err := ReadTargets(ctx, []File{f}, []string{"d"}, tt.overrides, nil)
			require.NoError(t, err)
			require.Equal(t, 1, len(g))
			require.Equal(t, []string{"d"}, g["default"].Targets)
			require.Equal(t, 1, len(m))
			require.Equal(t, tt.want, m["d"].Args)
		})
	}
}

func TestNestedInheritsWithGroup(t *testing.T) {
	ctx := context.TODO()

	f := File{
		Name: "docker-bake.hcl",
		Data: []byte(`
target "grandparent" {
  output = ["type=docker"]
  args = {
    BAR = "fuu"
  }
}
target "parent" {
  inherits = ["grandparent"]
  args = {
    FOO = "bar"
  }
}
target "child1" {
  inherits = ["parent"]
}
target "child2" {
  inherits = ["parent"]
  args = {
    FOO2 = "bar2"
  }
}
group "default" {
  targets = [
    "child1",
    "child2"
  ]
}`)}

	cases := []struct {
		name      string
		overrides []string
		wantch1   map[string]*string
		wantch2   map[string]*string
	}{
		{
			name:      "nested simple",
			overrides: nil,
			wantch1:   map[string]*string{"BAR": ptrstr("fuu"), "FOO": ptrstr("bar")},
			wantch2:   map[string]*string{"BAR": ptrstr("fuu"), "FOO": ptrstr("bar"), "FOO2": ptrstr("bar2")},
		},
		{
			name:      "nested with overrides first",
			overrides: []string{"grandparent.args.BAR=fii", "child1.args.FOO=baaar"},
			wantch1:   map[string]*string{"BAR": ptrstr("fii"), "FOO": ptrstr("baaar")},
			wantch2:   map[string]*string{"BAR": ptrstr("fii"), "FOO": ptrstr("bar"), "FOO2": ptrstr("bar2")},
		},
		{
			name:      "nested with overrides last",
			overrides: []string{"grandparent.args.BAR=fii", "child2.args.FOO=baaar"},
			wantch1:   map[string]*string{"BAR": ptrstr("fii"), "FOO": ptrstr("bar")},
			wantch2:   map[string]*string{"BAR": ptrstr("fii"), "FOO": ptrstr("baaar"), "FOO2": ptrstr("bar2")},
		},
	}
	for _, tt := range cases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			m, g, err := ReadTargets(ctx, []File{f}, []string{"default"}, tt.overrides, nil)
			require.NoError(t, err)
			require.Equal(t, 1, len(g))
			require.Equal(t, []string{"child1", "child2"}, g["default"].Targets)
			require.Equal(t, 2, len(m))
			require.Equal(t, tt.wantch1, m["child1"].Args)
			require.Equal(t, []string{"type=docker"}, m["child1"].Outputs)
			require.Equal(t, tt.wantch2, m["child2"].Args)
			require.Equal(t, []string{"type=docker"}, m["child2"].Outputs)
		})
	}
}

func TestTargetName(t *testing.T) {
	ctx := context.TODO()
	cases := []struct {
		target  string
		wantErr bool
	}{
		{
			target:  "a",
			wantErr: false,
		},
		{
			target:  "abc",
			wantErr: false,
		},
		{
			target:  "a/b",
			wantErr: true,
		},
		{
			target:  "a.b",
			wantErr: true,
		},
		{
			target:  "_a",
			wantErr: false,
		},
		{
			target:  "a_b",
			wantErr: false,
		},
		{
			target:  "AbC",
			wantErr: false,
		},
		{
			target:  "AbC-0123",
			wantErr: false,
		},
	}
	for _, tt := range cases {
		tt := tt
		t.Run(tt.target, func(t *testing.T) {
			_, _, err := ReadTargets(ctx, []File{{
				Name: "docker-bake.hcl",
				Data: []byte(`target "` + tt.target + `" {}`),
			}}, []string{tt.target}, nil, nil)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestNestedGroupsWithSameTarget(t *testing.T) {
	ctx := context.TODO()

	f := File{
		Name: "docker-bake.hcl",
		Data: []byte(`
group "a" {
  targets = ["b", "c"]
}

group "b" {
  targets = ["d"]
}

group "c" {
  targets = ["b"]
}

target "d" {
  context = "."
  dockerfile = "./testdockerfile"
}

group "e" {
  targets = ["a", "f"]
}

target "f" {
  context = "./foo"
}`)}

	cases := []struct {
		names   []string
		targets []string
		groups  []string
		count   int
	}{
		{
			names:   []string{"a"},
			targets: []string{"a"},
			groups:  []string{"default", "a", "b", "c"},
			count:   1,
		},
		{
			names:   []string{"b"},
			targets: []string{"b"},
			groups:  []string{"default", "b"},
			count:   1,
		},
		{
			names:   []string{"c"},
			targets: []string{"c"},
			groups:  []string{"default", "b", "c"},
			count:   1,
		},
		{
			names:   []string{"d"},
			targets: []string{"d"},
			groups:  []string{"default"},
			count:   1,
		},
		{
			names:   []string{"e"},
			targets: []string{"e"},
			groups:  []string{"default", "a", "b", "c", "e"},
			count:   2,
		},
		{
			names:   []string{"a", "e"},
			targets: []string{"a", "e"},
			groups:  []string{"default", "a", "b", "c", "e"},
			count:   2,
		},
	}
	for _, tt := range cases {
		tt := tt
		t.Run(strings.Join(tt.names, "+"), func(t *testing.T) {
			m, g, err := ReadTargets(ctx, []File{f}, tt.names, nil, nil)
			require.NoError(t, err)

			var gnames []string
			for _, g := range g {
				gnames = append(gnames, g.Name)
			}
			sort.Strings(gnames)
			sort.Strings(tt.groups)
			require.Equal(t, tt.groups, gnames)

			sort.Strings(g["default"].Targets)
			sort.Strings(tt.targets)
			require.Equal(t, tt.targets, g["default"].Targets)

			require.Equal(t, tt.count, len(m))
			require.Equal(t, ".", *m["d"].Context)
			require.Equal(t, "./testdockerfile", *m["d"].Dockerfile)
		})
	}
}

func TestUnknownExt(t *testing.T) {
	dt := []byte(`
		target "app" {
			context = "dir"
			args = {
				v1 = "foo"
			}
		}
		`)
	dt2 := []byte(`
services:
  app:
    build:
      dockerfile: Dockerfile-alternate
      args:
        v2: "bar"
`)

	c, err := ParseFiles([]File{
		{Data: dt, Name: "c1.foo"},
		{Data: dt2, Name: "c2.bar"},
	}, nil)
	require.NoError(t, err)

	require.Equal(t, 1, len(c.Targets))
	require.Equal(t, "app", c.Targets[0].Name)
	require.Equal(t, ptrstr("foo"), c.Targets[0].Args["v1"])
	require.Equal(t, ptrstr("bar"), c.Targets[0].Args["v2"])
	require.Equal(t, "dir", *c.Targets[0].Context)
	require.Equal(t, "Dockerfile-alternate", *c.Targets[0].Dockerfile)
}

func TestHCLNullVars(t *testing.T) {
	fp := File{
		Name: "docker-bake.hcl",
		Data: []byte(
			`variable "FOO" {
				default = null
			}
			variable "BAR" {
				default = null
			}
			target "default" {
				args = {
					foo = FOO
					bar = "baz"
				}
				labels = {
					"com.docker.app.bar" = BAR
					"com.docker.app.baz" = "foo"
				}
			}`),
	}

	ctx := context.TODO()
	m, _, err := ReadTargets(ctx, []File{fp}, []string{"default"}, nil, nil)
	require.NoError(t, err)

	require.Equal(t, 1, len(m))
	_, ok := m["default"]
	require.True(t, ok)

	_, err = TargetsToBuildOpt(m, &Input{})
	require.NoError(t, err)
	require.Equal(t, map[string]*string{"bar": ptrstr("baz")}, m["default"].Args)
	require.Equal(t, map[string]*string{"com.docker.app.baz": ptrstr("foo")}, m["default"].Labels)
}

func TestJSONNullVars(t *testing.T) {
	fp := File{
		Name: "docker-bake.json",
		Data: []byte(
			`{
				"variable": {
					"FOO": {
						"default": null
					}
				},
				"target": {
					"default": {
						"args": {
							"foo": "${FOO}",
							"bar": "baz"
						}
					}
				}
			}`),
	}

	ctx := context.TODO()
	m, _, err := ReadTargets(ctx, []File{fp}, []string{"default"}, nil, nil)
	require.NoError(t, err)

	require.Equal(t, 1, len(m))
	_, ok := m["default"]
	require.True(t, ok)

	_, err = TargetsToBuildOpt(m, &Input{})
	require.NoError(t, err)
	require.Equal(t, map[string]*string{"bar": ptrstr("baz")}, m["default"].Args)
}

func TestReadLocalFilesDefault(t *testing.T) {
	tests := []struct {
		filenames []string
		expected  []string
	}{
		{
			filenames: []string{"abc.yml", "docker-compose.yml"},
			expected:  []string{"docker-compose.yml"},
		},
		{
			filenames: []string{"test.foo", "compose.yml", "docker-bake.hcl"},
			expected:  []string{"compose.yml", "docker-bake.hcl"},
		},
		{
			filenames: []string{"compose.yaml", "docker-compose.yml", "docker-bake.hcl"},
			expected:  []string{"compose.yaml", "docker-compose.yml", "docker-bake.hcl"},
		},
		{
			filenames: []string{"test.txt", "compsoe.yaml"}, // intentional misspell
			expected:  []string{},
		},
	}
	pwd, err := os.Getwd()
	require.NoError(t, err)

	for _, tt := range tests {
		t.Run(strings.Join(tt.filenames, "-"), func(t *testing.T) {
			dir := t.TempDir()
			t.Cleanup(func() { _ = os.Chdir(pwd) })
			require.NoError(t, os.Chdir(dir))
			for _, tf := range tt.filenames {
				require.NoError(t, os.WriteFile(tf, []byte(tf), 0644))
			}
			files, err := ReadLocalFiles(nil, nil)
			require.NoError(t, err)
			if len(files) == 0 {
				require.Equal(t, len(tt.expected), len(files))
			} else {
				found := false
				for _, exp := range tt.expected {
					for _, f := range files {
						if f.Name == exp {
							found = true
							break
						}
					}
					require.True(t, found, exp)
				}
			}
		})
	}
}

func TestAttestDuplicates(t *testing.T) {
	fp := File{
		Name: "docker-bake.hcl",
		Data: []byte(
			`target "default" {
                attest = ["type=sbom", "type=sbom,generator=custom", "type=sbom,foo=bar", "type=provenance,mode=max"]
            }`),
	}
	ctx := context.TODO()

	m, _, err := ReadTargets(ctx, []File{fp}, []string{"default"}, nil, nil)
	require.Equal(t, []string{"type=sbom,foo=bar", "type=provenance,mode=max"}, m["default"].Attest)
	require.NoError(t, err)

	opts, err := TargetsToBuildOpt(m, &Input{})
	require.NoError(t, err)
	require.Equal(t, map[string]*string{
		"sbom":       ptrstr("type=sbom,foo=bar"),
		"provenance": ptrstr("type=provenance,mode=max"),
	}, opts["default"].Attests)

	m, _, err = ReadTargets(ctx, []File{fp}, []string{"default"}, []string{"*.attest=type=sbom,disabled=true"}, nil)
	require.Equal(t, []string{"type=sbom,disabled=true", "type=provenance,mode=max"}, m["default"].Attest)
	require.NoError(t, err)

	opts, err = TargetsToBuildOpt(m, &Input{})
	require.NoError(t, err)
	require.Equal(t, map[string]*string{
		"sbom":       nil,
		"provenance": ptrstr("type=provenance,mode=max"),
	}, opts["default"].Attests)
}

func TestAnnotations(t *testing.T) {
	fp := File{
		Name: "docker-bake.hcl",
		Data: []byte(
			`target "app" {
				output = ["type=image,name=foo"]
				annotations = ["manifest[linux/amd64]:foo=bar"]
			}`),
	}
	ctx := context.TODO()
	m, g, err := ReadTargets(ctx, []File{fp}, []string{"app"}, nil, nil)
	require.NoError(t, err)

	bo, err := TargetsToBuildOpt(m, &Input{})
	require.NoError(t, err)

	require.Equal(t, 1, len(g))
	require.Equal(t, []string{"app"}, g["default"].Targets)

	require.Equal(t, 1, len(m))
	require.Contains(t, m, "app")
	require.Equal(t, "type=image,name=foo", m["app"].Outputs[0])
	require.Equal(t, "manifest[linux/amd64]:foo=bar", m["app"].Annotations[0])

	require.Len(t, bo["app"].Exports, 1)
	require.Equal(t, "bar", bo["app"].Exports[0].Attrs["annotation-manifest[linux/amd64].foo"])
}
