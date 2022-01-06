package bake

import (
	"os"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseCompose(t *testing.T) {
	var dt = []byte(`
services:
  db:
    build: ./db
    command: ./entrypoint.sh
    image: docker.io/tonistiigi/db
  webapp:
    build:
      context: ./dir
      dockerfile: Dockerfile-alternate
      network:
        none
      args:
        buildno: 123
`)

	c, err := ParseCompose(dt)
	require.NoError(t, err)

	require.Equal(t, 1, len(c.Groups))
	require.Equal(t, c.Groups[0].Name, "default")
	sort.Strings(c.Groups[0].Targets)
	require.Equal(t, []string{"db", "webapp"}, c.Groups[0].Targets)

	require.Equal(t, 2, len(c.Targets))
	sort.Slice(c.Targets, func(i, j int) bool {
		return c.Targets[i].Name < c.Targets[j].Name
	})
	require.Equal(t, "db", c.Targets[0].Name)
	require.Equal(t, "./db", *c.Targets[0].Context)

	require.Equal(t, "webapp", c.Targets[1].Name)
	require.Equal(t, "./dir", *c.Targets[1].Context)
	require.Equal(t, "Dockerfile-alternate", *c.Targets[1].Dockerfile)
	require.Equal(t, 1, len(c.Targets[1].Args))
	require.Equal(t, "123", c.Targets[1].Args["buildno"])
	require.Equal(t, "none", *c.Targets[1].NetworkMode)
}

func TestNoBuildOutOfTreeService(t *testing.T) {
	var dt = []byte(`
services:
    external:
        image: "verycooldb:1337"
    webapp:
        build: ./db
`)
	c, err := ParseCompose(dt)
	require.NoError(t, err)
	require.Equal(t, 1, len(c.Groups))
}

func TestParseComposeTarget(t *testing.T) {
	var dt = []byte(`
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

	c, err := ParseCompose(dt)
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
	var dt = []byte(`
services:
  db:
    build:
      target: db
  webapp:
    build:
      context: .
      target: webapp
`)

	c, err := ParseCompose(dt)
	require.NoError(t, err)
	require.Equal(t, 2, len(c.Targets))
	sort.Slice(c.Targets, func(i, j int) bool {
		return c.Targets[i].Name < c.Targets[j].Name
	})
	require.Equal(t, c.Targets[0].Name, "db")
	require.Equal(t, "db", *c.Targets[0].Target)
	require.Equal(t, c.Targets[1].Name, "webapp")
	require.Equal(t, "webapp", *c.Targets[1].Target)
}

func TestBuildArgEnvCompose(t *testing.T) {
	var dt = []byte(`
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

	os.Setenv("FOO", "bar")
	defer os.Unsetenv("FOO")
	os.Setenv("BAR", "foo")
	defer os.Unsetenv("BAR")
	os.Setenv("ZZZ_BAR", "zzz_foo")
	defer os.Unsetenv("ZZZ_BAR")

	c, err := ParseCompose(dt)
	require.NoError(t, err)
	require.Equal(t, c.Targets[0].Args["FOO"], "bar")
	require.Equal(t, c.Targets[0].Args["BAR"], "zzz_foo")
	require.Equal(t, c.Targets[0].Args["BRB"], "FOO")
}

func TestBogusCompose(t *testing.T) {
	var dt = []byte(`
services:
  db:
    labels:
      - "foo"
  webapp:
    build:
      context: .
      target: webapp
`)

	_, err := ParseCompose(dt)
	require.Error(t, err)
	require.Contains(t, err.Error(), "has neither an image nor a build context specified: invalid compose project")
}

func TestAdvancedNetwork(t *testing.T) {
	var dt = []byte(`
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

	_, err := ParseCompose(dt)
	require.NoError(t, err)
}

func TestDependsOnList(t *testing.T) {
	var dt = []byte(`
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

	_, err := ParseCompose(dt)
	require.NoError(t, err)
}

func TestComposeExt(t *testing.T) {
	var dt = []byte(`
services:
  addon:
    image: ct-addon:bar
    build:
      context: .
      dockerfile: ./Dockerfile
      cache_from:
        - user/app:cache
      args:
        CT_ECR: foo
        CT_TAG: bar
      x-bake:
        tags:
          - ct-addon:foo
          - ct-addon:alp
        platforms:
          - linux/amd64
          - linux/arm64
        cache-from:
          - type=local,src=path/to/cache
        cache-to: local,dest=path/to/cache
        pull: true

  aws:
    image: ct-fake-aws:bar
    build:
      dockerfile: ./aws.Dockerfile
      args:
        CT_ECR: foo
        CT_TAG: bar
      x-bake:
        secret:
          - id=mysecret,src=/local/secret
          - id=mysecret2,src=/local/secret2
        ssh: default
        platforms: linux/arm64
        output: type=docker
        no-cache: true
`)

	c, err := ParseCompose(dt)
	require.NoError(t, err)
	require.Equal(t, 2, len(c.Targets))
	sort.Slice(c.Targets, func(i, j int) bool {
		return c.Targets[i].Name < c.Targets[j].Name
	})
	require.Equal(t, c.Targets[0].Args, map[string]string{"CT_ECR": "foo", "CT_TAG": "bar"})
	require.Equal(t, c.Targets[0].Tags, []string{"ct-addon:foo", "ct-addon:alp"})
	require.Equal(t, c.Targets[0].Platforms, []string{"linux/amd64", "linux/arm64"})
	require.Equal(t, c.Targets[0].CacheFrom, []string{"type=local,src=path/to/cache"})
	require.Equal(t, c.Targets[0].CacheTo, []string{"local,dest=path/to/cache"})
	require.Equal(t, c.Targets[0].Pull, newBool(true))
	require.Equal(t, c.Targets[1].Tags, []string{"ct-fake-aws:bar"})
	require.Equal(t, c.Targets[1].Secrets, []string{"id=mysecret,src=/local/secret", "id=mysecret2,src=/local/secret2"})
	require.Equal(t, c.Targets[1].SSH, []string{"default"})
	require.Equal(t, c.Targets[1].Platforms, []string{"linux/arm64"})
	require.Equal(t, c.Targets[1].Outputs, []string{"type=docker"})
	require.Equal(t, c.Targets[1].NoCache, newBool(true))
}

func TestEnvFile(t *testing.T) {
	envf, err := os.CreateTemp("", "env")
	require.NoError(t, err)
	defer os.Remove(envf.Name())

	_, err = envf.WriteString("FOO=bsdf -csdf\n")
	require.NoError(t, err)

	var dt = []byte(`
services:
  scratch:
    build:
     context: .
    env_file:
      - ` + envf.Name() + `
`)

	_, err = ParseCompose(dt)
	require.NoError(t, err)
}

func newBool(val bool) *bool {
	b := val
	return &b
}
