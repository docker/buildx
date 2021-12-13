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
