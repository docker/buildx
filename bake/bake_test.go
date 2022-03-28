package bake

import (
	"context"
	"os"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReadTargets(t *testing.T) {
	t.Parallel()

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
		m, g, err := ReadTargets(ctx, []File{fp}, []string{"webapp"}, nil, nil)
		require.NoError(t, err)
		require.Equal(t, 1, len(m))

		require.Equal(t, "Dockerfile.webapp", *m["webapp"].Dockerfile)
		require.Equal(t, ".", *m["webapp"].Context)
		require.Equal(t, "webDEP", m["webapp"].Args["VAR_INHERITED"])
		require.Equal(t, true, *m["webapp"].NoCache)
		require.Nil(t, m["webapp"].Pull)

		require.Equal(t, 1, len(g))
		require.Equal(t, []string{"webapp"}, g[0].Targets)
	})

	t.Run("InvalidTargetOverrides", func(t *testing.T) {
		_, _, err := ReadTargets(ctx, []File{fp}, []string{"webapp"}, []string{"nosuchtarget.context=foo"}, nil)
		require.NotNil(t, err)
		require.Equal(t, err.Error(), "could not find any target matching 'nosuchtarget'")
	})

	t.Run("ArgsOverrides", func(t *testing.T) {
		t.Run("leaf", func(t *testing.T) {
			os.Setenv("VAR_FROMENV"+t.Name(), "fromEnv")
			defer os.Unsetenv("VAR_FROM_ENV" + t.Name())

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

			require.Equal(t, m["webapp"].Args["VAR_SET"], "bananas")

			require.Equal(t, m["webapp"].Args["VAR_FROMENV"+t.Name()], "fromEnv")

			require.Equal(t, m["webapp"].Args["VAR_BOTH"], "webapp")
			require.Equal(t, m["webapp"].Args["VAR_INHERITED"], "override")

			require.Equal(t, 1, len(g))
			require.Equal(t, []string{"webapp"}, g[0].Targets)
		})

		// building leaf but overriding parent fields
		t.Run("parent", func(t *testing.T) {
			m, g, err := ReadTargets(ctx, []File{fp}, []string{"webapp"}, []string{
				"webDEP.args.VAR_INHERITED=override",
				"webDEP.args.VAR_BOTH=override",
			}, nil)

			require.NoError(t, err)
			require.Equal(t, m["webapp"].Args["VAR_INHERITED"], "override")
			require.Equal(t, m["webapp"].Args["VAR_BOTH"], "webapp")
			require.Equal(t, 1, len(g))
			require.Equal(t, []string{"webapp"}, g[0].Targets)
		})
	})

	t.Run("ContextOverride", func(t *testing.T) {
		_, _, err := ReadTargets(ctx, []File{fp}, []string{"webapp"}, []string{"webapp.context"}, nil)
		require.NotNil(t, err)

		m, g, err := ReadTargets(ctx, []File{fp}, []string{"webapp"}, []string{"webapp.context=foo"}, nil)
		require.NoError(t, err)
		require.Equal(t, "foo", *m["webapp"].Context)
		require.Equal(t, 1, len(g))
		require.Equal(t, []string{"webapp"}, g[0].Targets)
	})

	t.Run("NoCacheOverride", func(t *testing.T) {
		m, g, err := ReadTargets(ctx, []File{fp}, []string{"webapp"}, []string{"webapp.no-cache=false"}, nil)
		require.NoError(t, err)
		require.Equal(t, false, *m["webapp"].NoCache)
		require.Equal(t, 1, len(g))
		require.Equal(t, []string{"webapp"}, g[0].Targets)
	})

	t.Run("PullOverride", func(t *testing.T) {
		m, g, err := ReadTargets(ctx, []File{fp}, []string{"webapp"}, []string{"webapp.pull=false"}, nil)
		require.NoError(t, err)
		require.Equal(t, false, *m["webapp"].Pull)
		require.Equal(t, 1, len(g))
		require.Equal(t, []string{"webapp"}, g[0].Targets)
	})

	t.Run("PatternOverride", func(t *testing.T) {
		// same check for two cases
		multiTargetCheck := func(t *testing.T, m map[string]*Target, g []*Group, err error) {
			require.NoError(t, err)
			require.Equal(t, 2, len(m))
			require.Equal(t, "foo", *m["webapp"].Dockerfile)
			require.Equal(t, "webDEP", m["webapp"].Args["VAR_INHERITED"])
			require.Equal(t, "foo", *m["webDEP"].Dockerfile)
			require.Equal(t, "webDEP", m["webDEP"].Args["VAR_INHERITED"])
			require.Equal(t, 1, len(g))
			sort.Strings(g[0].Targets)
			require.Equal(t, []string{"webDEP", "webapp"}, g[0].Targets)
		}

		cases := []struct {
			name      string
			targets   []string
			overrides []string
			check     func(*testing.T, map[string]*Target, []*Group, error)
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
				check: func(t *testing.T, m map[string]*Target, g []*Group, err error) {
					require.NoError(t, err)
					require.Equal(t, 1, len(m))
					require.Equal(t, "foo", *m["webapp"].Dockerfile)
					require.Equal(t, "webDEP", m["webapp"].Args["VAR_INHERITED"])
					require.Equal(t, 1, len(g))
					require.Equal(t, []string{"webapp"}, g[0].Targets)
				},
			},
			{
				name:      "nomatch",
				targets:   []string{"webapp"},
				overrides: []string{"nomatch*.dockerfile=foo"},
				check: func(t *testing.T, m map[string]*Target, g []*Group, err error) {
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

	ctx := context.TODO()

	m, g, err := ReadTargets(ctx, []File{fp, fp2}, []string{"default"}, nil, nil)
	require.NoError(t, err)

	require.Equal(t, 3, len(m))
	_, ok := m["newservice"]

	require.True(t, ok)
	require.Equal(t, "Dockerfile.webapp", *m["webapp"].Dockerfile)
	require.Equal(t, ".", *m["webapp"].Context)
	require.Equal(t, "1", m["webapp"].Args["buildno"])
	require.Equal(t, "12", m["webapp"].Args["buildno2"])

	require.Equal(t, 1, len(g))
	sort.Strings(g[0].Targets)
	require.Equal(t, []string{"db", "newservice", "webapp"}, g[0].Targets)
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

	require.Equal(t, 1, len(m))
	_, ok := m["app"]
	require.True(t, ok)

	_, err = TargetsToBuildOpt(m, &Input{})
	require.NoError(t, err)

	require.Equal(t, "test", *m["app"].Dockerfile)
	require.Equal(t, "foo", *m["app"].Context)

	require.Equal(t, 1, len(g))
	require.Equal(t, []string{"app"}, g[0].Targets)
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
	require.Equal(t, []string{"image"}, g[0].Targets)
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
	require.Equal(t, 1, len(g))
	require.Equal(t, []string{"image"}, g[0].Targets)
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
	require.Equal(t, 1, len(g))
	require.Equal(t, []string{"image"}, g[0].Targets)
	require.Equal(t, 1, len(m))
	require.Equal(t, "test", *m["image"].Dockerfile)

	m, g, err = ReadTargets(ctx, []File{f}, []string{"foo", "foo"}, nil, nil)
	require.NoError(t, err)
	require.Equal(t, 1, len(g))
	require.Equal(t, []string{"image"}, g[0].Targets)
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
	require.Equal(t, []string{"image"}, g[0].Targets)
	require.Equal(t, 1, len(m))
	require.Equal(t, 1, len(m["image"].Outputs))
	require.Equal(t, "type=docker", m["image"].Outputs[0])

	m, g, err = ReadTargets(ctx, []File{fhcl}, []string{"image-release"}, nil, nil)
	require.NoError(t, err)
	require.Equal(t, 1, len(g))
	require.Equal(t, []string{"image-release"}, g[0].Targets)
	require.Equal(t, 1, len(m))
	require.Equal(t, 1, len(m["image-release"].Outputs))
	require.Equal(t, "type=image,push=true", m["image-release"].Outputs[0])

	m, g, err = ReadTargets(ctx, []File{fhcl}, []string{"image", "image-release"}, nil, nil)
	require.NoError(t, err)
	require.Equal(t, 1, len(g))
	require.Equal(t, []string{"image", "image-release"}, g[0].Targets)
	require.Equal(t, 2, len(m))
	require.Equal(t, ".", *m["image"].Context)
	require.Equal(t, 1, len(m["image-release"].Outputs))
	require.Equal(t, "type=image,push=true", m["image-release"].Outputs[0])

	m, g, err = ReadTargets(ctx, []File{fyml, fhcl}, []string{"default"}, nil, nil)
	require.NoError(t, err)
	require.Equal(t, 1, len(g))
	require.Equal(t, []string{"image"}, g[0].Targets)
	require.Equal(t, 1, len(m))
	require.Equal(t, ".", *m["image"].Context)

	m, g, err = ReadTargets(ctx, []File{fjson}, []string{"default"}, nil, nil)
	require.NoError(t, err)
	require.Equal(t, 1, len(g))
	require.Equal(t, []string{"image"}, g[0].Targets)
	require.Equal(t, 1, len(m))
	require.Equal(t, ".", *m["image"].Context)

	m, g, err = ReadTargets(ctx, []File{fyml}, []string{"default"}, nil, nil)
	require.NoError(t, err)
	require.Equal(t, 1, len(g))
	sort.Strings(g[0].Targets)
	require.Equal(t, []string{"addon", "aws"}, g[0].Targets)
	require.Equal(t, 2, len(m))
	require.Equal(t, "./Dockerfile", *m["addon"].Dockerfile)
	require.Equal(t, "./aws.Dockerfile", *m["aws"].Dockerfile)

	m, g, err = ReadTargets(ctx, []File{fyml, fhcl}, []string{"addon", "aws"}, nil, nil)
	require.NoError(t, err)
	require.Equal(t, 1, len(g))
	sort.Strings(g[0].Targets)
	require.Equal(t, []string{"addon", "aws"}, g[0].Targets)
	require.Equal(t, 2, len(m))
	require.Equal(t, "./Dockerfile", *m["addon"].Dockerfile)
	require.Equal(t, "./aws.Dockerfile", *m["aws"].Dockerfile)

	m, g, err = ReadTargets(ctx, []File{fyml, fhcl}, []string{"addon", "aws", "image"}, nil, nil)
	require.NoError(t, err)
	require.Equal(t, 1, len(g))
	sort.Strings(g[0].Targets)
	require.Equal(t, []string{"addon", "aws", "image"}, g[0].Targets)
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
	require.Equal(t, 1, len(g))
	require.Equal(t, []string{"foo"}, g[0].Targets)
	require.Equal(t, 1, len(m))
	require.Equal(t, "bar", *m["foo"].Dockerfile)

	m, g, err = ReadTargets(ctx, []File{f}, []string{"foo", "foo"}, nil, nil)
	require.NoError(t, err)
	require.Equal(t, 1, len(g))
	require.Equal(t, []string{"foo"}, g[0].Targets)
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
	require.Equal(t, 1, len(g))
	require.Equal(t, []string{"foo", "image"}, g[0].Targets)
	require.Equal(t, 2, len(m))
	require.Equal(t, "bar", *m["foo"].Dockerfile)
	require.Equal(t, "type=docker", m["image"].Outputs[0])

	m, g, err = ReadTargets(ctx, []File{f}, []string{"foo", "image"}, nil, nil)
	require.NoError(t, err)
	require.Equal(t, 1, len(g))
	require.Equal(t, []string{"foo", "image"}, g[0].Targets)
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
		want      map[string]string
	}{
		{
			name:      "nested simple",
			overrides: nil,
			want:      map[string]string{"bar": "234", "baz": "890", "foo": "123"},
		},
		{
			name:      "nested with overrides first",
			overrides: []string{"a.args.foo=321", "b.args.bar=432"},
			want:      map[string]string{"bar": "234", "baz": "890", "foo": "321"},
		},
		{
			name:      "nested with overrides last",
			overrides: []string{"a.args.foo=321", "c.args.bar=432"},
			want:      map[string]string{"bar": "432", "baz": "890", "foo": "321"},
		},
	}
	for _, tt := range cases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			m, g, err := ReadTargets(ctx, []File{f}, []string{"d"}, tt.overrides, nil)
			require.NoError(t, err)
			require.Equal(t, 1, len(g))
			require.Equal(t, []string{"d"}, g[0].Targets)
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
		wantch1   map[string]string
		wantch2   map[string]string
	}{
		{
			name:      "nested simple",
			overrides: nil,
			wantch1:   map[string]string{"BAR": "fuu", "FOO": "bar"},
			wantch2:   map[string]string{"BAR": "fuu", "FOO": "bar", "FOO2": "bar2"},
		},
		{
			name:      "nested with overrides first",
			overrides: []string{"grandparent.args.BAR=fii", "child1.args.FOO=baaar"},
			wantch1:   map[string]string{"BAR": "fii", "FOO": "baaar"},
			wantch2:   map[string]string{"BAR": "fii", "FOO": "bar", "FOO2": "bar2"},
		},
		{
			name:      "nested with overrides last",
			overrides: []string{"grandparent.args.BAR=fii", "child2.args.FOO=baaar"},
			wantch1:   map[string]string{"BAR": "fii", "FOO": "bar"},
			wantch2:   map[string]string{"BAR": "fii", "FOO": "baaar", "FOO2": "bar2"},
		},
	}
	for _, tt := range cases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			m, g, err := ReadTargets(ctx, []File{f}, []string{"default"}, tt.overrides, nil)
			require.NoError(t, err)
			require.Equal(t, 1, len(g))
			require.Equal(t, []string{"child1", "child2"}, g[0].Targets)
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
		name     string
		targets  []string
		ntargets int
	}{
		{
			name:     "a",
			targets:  []string{"b", "c"},
			ntargets: 1,
		},
		{
			name:     "b",
			targets:  []string{"d"},
			ntargets: 1,
		},
		{
			name:     "c",
			targets:  []string{"b"},
			ntargets: 1,
		},
		{
			name:     "d",
			targets:  []string{"d"},
			ntargets: 1,
		},
		{
			name:     "e",
			targets:  []string{"a", "f"},
			ntargets: 2,
		},
	}
	for _, tt := range cases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			m, g, err := ReadTargets(ctx, []File{f}, []string{tt.name}, nil, nil)
			require.NoError(t, err)
			require.Equal(t, 1, len(g))
			require.Equal(t, tt.targets, g[0].Targets)
			require.Equal(t, tt.ntargets, len(m))
			require.Equal(t, ".", *m["d"].Context)
			require.Equal(t, "./testdockerfile", *m["d"].Dockerfile)
		})
	}
}
