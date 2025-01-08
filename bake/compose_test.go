package bake

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCompose(t *testing.T) {
	dt := []byte(`
services:
  db:
    build: ./db
    command: ./entrypoint.sh
    image: docker.io/tonistiigi/db
  webapp:
    build:
      context: ./dir
      additional_contexts:
        foo: ./bar
      dockerfile: Dockerfile-alternate
      network:
        none
      args:
        buildno: 123
      cache_from:
        - type=local,src=path/to/cache
      cache_to:
        - type=local,dest=path/to/cache
      ssh:
        - key=/path/to/key
        - default
      secrets:
        - token
        - aws
  webapp2:
    profiles:
      - test
    build:
      context: ./dir
      dockerfile_inline: |
        FROM alpine
secrets:
  token:
    environment: ENV_TOKEN
  aws:
    file: /root/.aws/credentials
`)

	c, err := ParseCompose([]composetypes.ConfigFile{{Content: dt}}, nil)
	require.NoError(t, err)

	require.Equal(t, 1, len(c.Groups))
	require.Equal(t, "default", c.Groups[0].Name)
	sort.Strings(c.Groups[0].Targets)
	require.Equal(t, []string{"db", "webapp", "webapp2"}, c.Groups[0].Targets)

	require.Equal(t, 3, len(c.Targets))
	sort.Slice(c.Targets, func(i, j int) bool {
		return c.Targets[i].Name < c.Targets[j].Name
	})
	require.Equal(t, "db", c.Targets[0].Name)
	require.Equal(t, "db", *c.Targets[0].Context)
	require.Equal(t, []string{"docker.io/tonistiigi/db"}, c.Targets[0].Tags)

	require.Equal(t, "webapp", c.Targets[1].Name)
	require.Equal(t, "dir", *c.Targets[1].Context)
	require.Equal(t, map[string]string{"foo": "bar"}, c.Targets[1].Contexts)
	require.Equal(t, "Dockerfile-alternate", *c.Targets[1].Dockerfile)
	require.Equal(t, 1, len(c.Targets[1].Args))
	require.Equal(t, ptrstr("123"), c.Targets[1].Args["buildno"])
	require.Equal(t, []string{"type=local,src=path/to/cache"}, stringify(c.Targets[1].CacheFrom))
	require.Equal(t, []string{"type=local,dest=path/to/cache"}, stringify(c.Targets[1].CacheTo))
	require.Equal(t, "none", *c.Targets[1].NetworkMode)
	require.Equal(t, []string{"default", "key=/path/to/key"}, stringify(c.Targets[1].SSH))
	require.Equal(t, []string{
		"id=aws,src=/root/.aws/credentials",
		"id=token,env=ENV_TOKEN",
	}, stringify(c.Targets[1].Secrets))

	require.Equal(t, "webapp2", c.Targets[2].Name)
	require.Equal(t, "dir", *c.Targets[2].Context)
	require.Equal(t, "FROM alpine\n", *c.Targets[2].DockerfileInline)
}

func TestNoBuildOutOfTreeService(t *testing.T) {
	dt := []byte(`
services:
    external:
        image: "verycooldb:1337"
    webapp:
        build: ./db
`)
	c, err := ParseCompose([]composetypes.ConfigFile{{Content: dt}}, nil)
	require.NoError(t, err)
	require.Equal(t, 1, len(c.Groups))
	require.Equal(t, 1, len(c.Targets))
}

func TestParseComposeTarget(t *testing.T) {
	dt := []byte(`
services:
  db:
    build:
      context: ./db
      target: db
  webapp:
    build:
      context: .
      target: webapp
`)

	c, err := ParseCompose([]composetypes.ConfigFile{{Content: dt}}, nil)
	require.NoError(t, err)

	require.Equal(t, 2, len(c.Targets))
	sort.Slice(c.Targets, func(i, j int) bool {
		return c.Targets[i].Name < c.Targets[j].Name
	})
	require.Equal(t, "db", c.Targets[0].Name)
	require.Equal(t, "db", *c.Targets[0].Target)
	require.Equal(t, "webapp", c.Targets[1].Name)
	require.Equal(t, "webapp", *c.Targets[1].Target)
}

func TestComposeBuildWithoutContext(t *testing.T) {
	dt := []byte(`
services:
  db:
    build:
      target: db
  webapp:
    build:
      context: .
      target: webapp
`)

	c, err := ParseCompose([]composetypes.ConfigFile{{Content: dt}}, nil)
	require.NoError(t, err)
	require.Equal(t, 2, len(c.Targets))
	sort.Slice(c.Targets, func(i, j int) bool {
		return c.Targets[i].Name < c.Targets[j].Name
	})
	require.Equal(t, "db", c.Targets[0].Name)
	require.Equal(t, "db", *c.Targets[0].Target)
	require.Equal(t, "webapp", c.Targets[1].Name)
	require.Equal(t, "webapp", *c.Targets[1].Target)
}

func TestBuildArgEnvCompose(t *testing.T) {
	dt := []byte(`
version: "3.8"
services:
  example:
    image: example
    build:
      context: .
      dockerfile: Dockerfile
      args:
        FOO:
        BAR: $ZZZ_BAR
        BRB: FOO
`)

	t.Setenv("FOO", "bar")
	t.Setenv("BAR", "foo")
	t.Setenv("ZZZ_BAR", "zzz_foo")

	c, err := ParseCompose([]composetypes.ConfigFile{{Content: dt}}, sliceToMap(os.Environ()))
	require.NoError(t, err)
	require.Equal(t, ptrstr("bar"), c.Targets[0].Args["FOO"])
	require.Equal(t, ptrstr("zzz_foo"), c.Targets[0].Args["BAR"])
	require.Equal(t, ptrstr("FOO"), c.Targets[0].Args["BRB"])
}

func TestInconsistentComposeFile(t *testing.T) {
	dt := []byte(`
services:
  webapp:
    entrypoint: echo 1
`)

	_, err := ParseCompose([]composetypes.ConfigFile{{Content: dt}}, nil)
	require.Error(t, err)
}

func TestAdvancedNetwork(t *testing.T) {
	dt := []byte(`
services:
  db:
    networks:
      - example.com
    build:
      context: ./db
      target: db

networks:
  example.com:
    name: example.com
    driver: bridge
    ipam:
      config:
        - subnet: 10.5.0.0/24
          ip_range: 10.5.0.0/24
          gateway: 10.5.0.254
`)

	_, err := ParseCompose([]composetypes.ConfigFile{{Content: dt}}, nil)
	require.NoError(t, err)
}

func TestTags(t *testing.T) {
	dt := []byte(`
services:
  example:
    image: example
    build:
      context: .
      dockerfile: Dockerfile
      tags:
        - foo
        - bar
`)

	c, err := ParseCompose([]composetypes.ConfigFile{{Content: dt}}, nil)
	require.NoError(t, err)
	require.Equal(t, []string{"foo", "bar"}, c.Targets[0].Tags)
}

func TestDependsOnList(t *testing.T) {
	dt := []byte(`
version: "3.8"

services:
  example-container:
    image: example/fails:latest
    build:
      context: .
      dockerfile: Dockerfile
    depends_on:
      other-container:
        condition: service_healthy
    networks:
      default:
        aliases:
          - integration-tests

  other-container:
    image: example/other:latest
    healthcheck:
      test: ["CMD", "echo", "success"]
      retries: 5
      interval: 5s
      timeout: 10s
      start_period: 5s

networks:
  default:
    name: test-net
`)

	_, err := ParseCompose([]composetypes.ConfigFile{{Content: dt}}, nil)
	require.NoError(t, err)
}

func TestComposeExt(t *testing.T) {
	dt := []byte(`
services:
  addon:
    image: ct-addon:bar
    build:
      context: .
      dockerfile: ./Dockerfile
      cache_from:
        - user/app:cache
      cache_to:
        - user/app:cache
      tags:
        - ct-addon:baz
      ssh:
        key: /path/to/key
      args:
        CT_ECR: foo
        CT_TAG: bar
      x-bake:
        contexts:
          alpine: docker-image://alpine:3.13
        tags:
          - ct-addon:foo
          - ct-addon:alp
        ssh:
          - default
          - other=path/to/otherkey
        platforms:
          - linux/amd64
          - linux/arm64
        cache-from:
          - type=local,src=path/to/cache
        cache-to:
          - type=local,dest=path/to/cache
        pull: true

  aws:
    image: ct-fake-aws:bar
    build:
      dockerfile: ./aws.Dockerfile
      args:
        CT_ECR: foo
        CT_TAG: bar
      shm_size: 128m
      ulimits:
        nofile:
          soft: 1024
          hard: 1024
      x-bake:
        secret:
          - id=mysecret,src=/local/secret
          - id=mysecret2,src=/local/secret2
        ssh: default
        platforms: linux/arm64
        output: type=docker
        no-cache: true
`)

	c, err := ParseCompose([]composetypes.ConfigFile{{Content: dt}}, nil)
	require.NoError(t, err)
	require.Equal(t, 2, len(c.Targets))
	sort.Slice(c.Targets, func(i, j int) bool {
		return c.Targets[i].Name < c.Targets[j].Name
	})
	require.Equal(t, map[string]*string{"CT_ECR": ptrstr("foo"), "CT_TAG": ptrstr("bar")}, c.Targets[0].Args)
	require.Equal(t, []string{"ct-addon:baz", "ct-addon:foo", "ct-addon:alp"}, c.Targets[0].Tags)
	require.Equal(t, []string{"linux/amd64", "linux/arm64"}, c.Targets[0].Platforms)
	require.Equal(t, []string{"type=local,src=path/to/cache", "user/app:cache"}, stringify(c.Targets[0].CacheFrom))
	require.Equal(t, []string{"type=local,dest=path/to/cache", "user/app:cache"}, stringify(c.Targets[0].CacheTo))
	require.Equal(t, []string{"default", "key=/path/to/key", "other=path/to/otherkey"}, stringify(c.Targets[0].SSH))
	require.Equal(t, newBool(true), c.Targets[0].Pull)
	require.Equal(t, map[string]string{"alpine": "docker-image://alpine:3.13"}, c.Targets[0].Contexts)
	require.Equal(t, []string{"ct-fake-aws:bar"}, c.Targets[1].Tags)
	require.Equal(t, []string{"id=mysecret,src=/local/secret", "id=mysecret2,src=/local/secret2"}, stringify(c.Targets[1].Secrets))
	require.Equal(t, []string{"default"}, stringify(c.Targets[1].SSH))
	require.Equal(t, []string{"linux/arm64"}, c.Targets[1].Platforms)
	require.Equal(t, []string{"type=docker"}, stringify(c.Targets[1].Outputs))
	require.Equal(t, newBool(true), c.Targets[1].NoCache)
	require.Equal(t, ptrstr("128MiB"), c.Targets[1].ShmSize)
	require.Equal(t, []string{"nofile=1024:1024"}, c.Targets[1].Ulimits)
}

func TestComposeExtDedup(t *testing.T) {
	dt := []byte(`
services:
  webapp:
    image: app:bar
    build:
      cache_from:
        - user/app:cache
      cache_to:
        - user/app:cache
      tags:
        - ct-addon:foo
      ssh:
        - default
      x-bake:
        tags:
          - ct-addon:foo
          - ct-addon:baz
        cache-from:
          - user/app:cache
          - type=local,src=path/to/cache
        cache-to:
          - type=local,dest=path/to/cache
        ssh:
          - default
          - key=path/to/key
`)

	c, err := ParseCompose([]composetypes.ConfigFile{{Content: dt}}, nil)
	require.NoError(t, err)
	require.Equal(t, 1, len(c.Targets))
	require.Equal(t, []string{"ct-addon:foo", "ct-addon:baz"}, c.Targets[0].Tags)
	require.Equal(t, []string{"type=local,src=path/to/cache", "user/app:cache"}, stringify(c.Targets[0].CacheFrom))
	require.Equal(t, []string{"type=local,dest=path/to/cache", "user/app:cache"}, stringify(c.Targets[0].CacheTo))
	require.Equal(t, []string{"default", "key=path/to/key"}, stringify(c.Targets[0].SSH))
}

func TestEnv(t *testing.T) {
	envf, err := os.CreateTemp("", "env")
	require.NoError(t, err)
	defer os.Remove(envf.Name())

	_, err = envf.WriteString("FOO=bsdf -csdf\n")
	require.NoError(t, err)

	dt := []byte(`
services:
  scratch:
    build:
     context: .
     args:
        CT_ECR: foo
        FOO:
        NODE_ENV:
    environment:
      - NODE_ENV=test
      - AWS_ACCESS_KEY_ID=dummy
      - AWS_SECRET_ACCESS_KEY=dummy
    env_file:
      - ` + envf.Name() + `
`)

	c, err := ParseCompose([]composetypes.ConfigFile{{Content: dt}}, nil)
	require.NoError(t, err)
	require.Equal(t, map[string]*string{"CT_ECR": ptrstr("foo"), "FOO": ptrstr("bsdf -csdf"), "NODE_ENV": ptrstr("test")}, c.Targets[0].Args)
}

func TestDotEnv(t *testing.T) {
	tmpdir := t.TempDir()

	err := os.WriteFile(filepath.Join(tmpdir, ".env"), []byte("FOO=bar"), 0644)
	require.NoError(t, err)

	dt := []byte(`
services:
  scratch:
    build:
     context: .
     args:
        FOO:
`)

	chdir(t, tmpdir)
	c, err := ParseComposeFiles([]File{{
		Name: "docker-compose.yml",
		Data: dt,
	}})
	require.NoError(t, err)
	require.Equal(t, map[string]*string{"FOO": ptrstr("bar")}, c.Targets[0].Args)
}

func TestPorts(t *testing.T) {
	dt := []byte(`
services:
  foo:
    build:
     context: .
    ports:
      - 3306:3306
  bar:
    build:
     context: .
    ports:
      - mode: ingress
        target: 3306
        published: "3306"
        protocol: tcp
`)
	_, err := ParseCompose([]composetypes.ConfigFile{{Content: dt}}, nil)
	require.NoError(t, err)
}

func newBool(val bool) *bool {
	b := val
	return &b
}

func TestServiceName(t *testing.T) {
	cases := []struct {
		svc     string
		wantErr bool
	}{
		{
			svc:     "a",
			wantErr: false,
		},
		{
			svc:     "abc",
			wantErr: false,
		},
		{
			svc:     "a.b",
			wantErr: false,
		},
		{
			svc:     "_a",
			wantErr: false,
		},
		{
			svc:     "a_b",
			wantErr: false,
		},
		{
			svc:     "AbC",
			wantErr: false,
		},
		{
			svc:     "AbC-0123",
			wantErr: false,
		},
	}
	for _, tt := range cases {
		tt := tt
		t.Run(tt.svc, func(t *testing.T) {
			_, err := ParseCompose([]composetypes.ConfigFile{{Content: []byte(`
services:
  ` + tt.svc + `:
    build:
      context: .
`)}}, nil)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateComposeSecret(t *testing.T) {
	cases := []struct {
		name    string
		dt      []byte
		wantErr bool
	}{
		{
			name: "secret set by file",
			dt: []byte(`
secrets:
  foo:
    file: .secret
`),
			wantErr: false,
		},
		{
			name: "secret set by environment",
			dt: []byte(`
secrets:
  foo:
    environment: TOKEN
`),
			wantErr: false,
		},
		{
			name: "external secret",
			dt: []byte(`
secrets:
  foo:
    external: true
`),
			wantErr: false,
		},
		{
			name: "unset secret",
			dt: []byte(`
secrets:
  foo: {}
`),
			wantErr: true,
		},
		{
			name: "undefined secret",
			dt: []byte(`
services:
  foo:
    build:
      secrets:
        - token
`),
			wantErr: true,
		},
	}
	for _, tt := range cases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseCompose([]composetypes.ConfigFile{{Content: tt.dt}}, nil)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateComposeFile(t *testing.T) {
	cases := []struct {
		name      string
		fn        string
		dt        []byte
		isCompose bool
		wantErr   bool
	}{
		{
			name: "empty service",
			fn:   "docker-compose.yml",
			dt: []byte(`
services:
  foo:
`),
			isCompose: true,
			wantErr:   true,
		},
		{
			name: "build",
			fn:   "docker-compose.yml",
			dt: []byte(`
services:
  foo:
    build: .
`),
			isCompose: true,
			wantErr:   false,
		},
		{
			name: "image",
			fn:   "docker-compose.yml",
			dt: []byte(`
services:
  simple:
    image: nginx
`),
			isCompose: true,
			wantErr:   false,
		},
		{
			name: "unknown ext",
			fn:   "docker-compose.foo",
			dt: []byte(`
services:
  simple:
    image: nginx
`),
			isCompose: true,
			wantErr:   false,
		},
		{
			name: "hcl",
			fn:   "docker-bake.hcl",
			dt: []byte(`
target "default" {
  dockerfile = "test"
}
`),
			isCompose: false,
			wantErr:   false,
		},
	}
	for _, tt := range cases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			isCompose, err := validateComposeFile(tt.dt, tt.fn)
			assert.Equal(t, tt.isCompose, isCompose)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestComposeNullArgs(t *testing.T) {
	dt := []byte(`
services:
  scratch:
    build:
     context: .
     args:
        FOO: null
        bar: "baz"
`)

	c, err := ParseCompose([]composetypes.ConfigFile{{Content: dt}}, nil)
	require.NoError(t, err)
	require.Equal(t, map[string]*string{"bar": ptrstr("baz")}, c.Targets[0].Args)
}

func TestDependsOn(t *testing.T) {
	dt := []byte(`
services:
  foo:
    build:
     context: .
    ports:
      - 3306:3306
    depends_on:
      - bar
  bar:
    build:
     context: .
`)
	_, err := ParseCompose([]composetypes.ConfigFile{{Content: dt}}, nil)
	require.NoError(t, err)
}

func TestInclude(t *testing.T) {
	tmpdir := t.TempDir()

	err := os.WriteFile(filepath.Join(tmpdir, "compose-foo.yml"), []byte(`
services:
  foo:
    build:
     context: .
     target: buildfoo
    ports:
      - 3306:3306
`), 0644)
	require.NoError(t, err)

	dt := []byte(`
include:
  - compose-foo.yml

services:
  bar:
    build:
     context: .
     target: buildbar
`)

	chdir(t, tmpdir)
	c, err := ParseComposeFiles([]File{{
		Name: "composetypes.yml",
		Data: dt,
	}})
	require.NoError(t, err)

	require.Equal(t, 2, len(c.Targets))
	sort.Slice(c.Targets, func(i, j int) bool {
		return c.Targets[i].Name < c.Targets[j].Name
	})
	require.Equal(t, "bar", c.Targets[0].Name)
	require.Equal(t, "buildbar", *c.Targets[0].Target)
	require.Equal(t, "foo", c.Targets[1].Name)
	require.Equal(t, "buildfoo", *c.Targets[1].Target)
}

func TestDevelop(t *testing.T) {
	dt := []byte(`
services:
  scratch:
    build:
     context: ./webapp
    develop:
      watch: 
        - path: ./webapp/html
          action: sync
          target: /var/www
          ignore:
            - node_modules/
`)

	_, err := ParseCompose([]composetypes.ConfigFile{{Content: dt}}, nil)
	require.NoError(t, err)
}

func TestCgroup(t *testing.T) {
	dt := []byte(`
services:
  scratch:
    build:
     context: ./webapp
    cgroup: private
`)

	_, err := ParseCompose([]composetypes.ConfigFile{{Content: dt}}, nil)
	require.NoError(t, err)
}

func TestProjectName(t *testing.T) {
	dt := []byte(`
services:
  scratch:
    build:
     context: ./webapp
     args:
       PROJECT_NAME: ${COMPOSE_PROJECT_NAME}
`)

	t.Run("default", func(t *testing.T) {
		c, err := ParseCompose([]composetypes.ConfigFile{{Content: dt}}, nil)
		require.NoError(t, err)
		require.Len(t, c.Targets, 1)
		require.Len(t, c.Targets[0].Args, 1)
		require.Equal(t, map[string]*string{"PROJECT_NAME": ptrstr("bake")}, c.Targets[0].Args)
	})

	t.Run("env", func(t *testing.T) {
		c, err := ParseCompose([]composetypes.ConfigFile{{Content: dt}}, map[string]string{"COMPOSE_PROJECT_NAME": "foo"})
		require.NoError(t, err)
		require.Len(t, c.Targets, 1)
		require.Len(t, c.Targets[0].Args, 1)
		require.Equal(t, map[string]*string{"PROJECT_NAME": ptrstr("foo")}, c.Targets[0].Args)
	})
}

// chdir changes the current working directory to the named directory,
// and then restore the original working directory at the end of the test.
func chdir(t *testing.T, dir string) {
	olddir, err := os.Getwd()
	if err != nil {
		t.Fatalf("chdir: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(olddir); err != nil {
			t.Errorf("chdir to original working directory %s: %v", olddir, err)
			os.Exit(1)
		}
	})
}
