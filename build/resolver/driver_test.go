package resolver

import (
	"context"
	"sort"
	"testing"

	"github.com/containerd/platforms"
	"github.com/docker/buildx/builder"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
)

func TestFindDriverSanity(t *testing.T) {
	r := makeTestResolver(map[string][]ocispecs.Platform{
		"builder-amd64": {platforms.DefaultSpec()},
	})

	res, perfect, err := r.resolve(context.TODO(), []ocispecs.Platform{platforms.DefaultSpec()}, nil, platforms.OnlyStrict, nil)
	require.NoError(t, err)
	require.True(t, perfect)
	require.Len(t, res, 1)
	require.Equal(t, 0, res[0].driverIndex)
	require.Equal(t, "builder-amd64", res[0].Node().Builder)
	require.Equal(t, []ocispecs.Platform{platforms.DefaultSpec()}, res[0].Platforms())
}

func TestFindDriverEmpty(t *testing.T) {
	r := makeTestResolver(nil)

	res, perfect, err := r.resolve(context.TODO(), []ocispecs.Platform{platforms.DefaultSpec()}, nil, platforms.Only, nil)
	require.NoError(t, err)
	require.True(t, perfect)
	require.Nil(t, res)
}

func TestFindDriverWeirdName(t *testing.T) {
	r := makeTestResolver(map[string][]ocispecs.Platform{
		"builder-amd64": {platforms.MustParse("linux/amd64")},
		"builder-beta":  {platforms.MustParse("linux/beta")},
	})

	// find first platform
	res, perfect, err := r.resolve(context.TODO(), []ocispecs.Platform{platforms.MustParse("linux/beta")}, nil, platforms.Only, nil)
	require.NoError(t, err)
	require.True(t, perfect)
	require.Len(t, res, 1)
	require.Equal(t, 1, res[0].driverIndex)
	require.Equal(t, "builder-beta", res[0].Node().Builder)
}

func TestFindDriverUnknown(t *testing.T) {
	r := makeTestResolver(map[string][]ocispecs.Platform{
		"builder-amd64": {platforms.MustParse("linux/amd64")},
	})

	res, perfect, err := r.resolve(context.TODO(), []ocispecs.Platform{platforms.MustParse("linux/riscv64")}, nil, platforms.Only, nil)
	require.NoError(t, err)
	require.False(t, perfect)
	require.Len(t, res, 1)
	require.Equal(t, 0, res[0].driverIndex)
	require.Equal(t, "builder-amd64", res[0].Node().Builder)
}

func TestSelectNodeSinglePlatform(t *testing.T) {
	r := makeTestResolver(map[string][]ocispecs.Platform{
		"builder-amd64":   {platforms.MustParse("linux/amd64")},
		"builder-riscv64": {platforms.MustParse("linux/riscv64")},
	})

	// Request linux/amd64 platform, should match builder-amd64
	res, perfect, err := r.resolve(context.TODO(), []ocispecs.Platform{platforms.MustParse("linux/amd64")}, nil, platforms.Only, nil)
	require.NoError(t, err)
	require.True(t, perfect)
	require.Len(t, res, 1)
	require.Equal(t, 0, res[0].driverIndex)
	require.Equal(t, "builder-amd64", res[0].Node().Builder)

	// Request linux/riscv64 platform, should match builder-riscv64
	res, perfect, err = r.resolve(context.TODO(), []ocispecs.Platform{platforms.MustParse("linux/riscv64")}, nil, platforms.Only, nil)
	require.NoError(t, err)
	require.True(t, perfect)
	require.Len(t, res, 1)
	require.Equal(t, 1, res[0].driverIndex)
	require.Equal(t, "builder-riscv64", res[0].Node().Builder)

	// Request unknown platform like linux/unknown, should default to first builder (builder-amd64)
	res, perfect, err = r.resolve(context.TODO(), []ocispecs.Platform{platforms.MustParse("linux/unknown")}, nil, platforms.Only, nil)
	require.NoError(t, err)
	require.False(t, perfect)
	require.Len(t, res, 1)
	require.Equal(t, 0, res[0].driverIndex)
	require.Equal(t, "builder-amd64", res[0].Node().Builder)
}

func TestSelectNodeMultiPlatform(t *testing.T) {
	r := makeTestResolver(map[string][]ocispecs.Platform{
		"builder-amd64-arm64": {platforms.MustParse("linux/amd64"), platforms.MustParse("linux/arm64")},
		"builder-riscv64":     {platforms.MustParse("linux/riscv64")},
	})

	res, perfect, err := r.resolve(context.TODO(), []ocispecs.Platform{platforms.MustParse("linux/amd64")}, nil, platforms.Only, nil)
	require.NoError(t, err)
	require.True(t, perfect)
	require.Len(t, res, 1)
	require.Equal(t, 0, res[0].driverIndex)
	require.Equal(t, "builder-amd64-arm64", res[0].Node().Builder)

	res, perfect, err = r.resolve(context.TODO(), []ocispecs.Platform{platforms.MustParse("linux/arm64")}, nil, platforms.Only, nil)
	require.NoError(t, err)
	require.True(t, perfect)
	require.Len(t, res, 1)
	require.Equal(t, 0, res[0].driverIndex)
	require.Equal(t, "builder-amd64-arm64", res[0].Node().Builder)

	res, perfect, err = r.resolve(context.TODO(), []ocispecs.Platform{platforms.MustParse("linux/riscv64")}, nil, platforms.Only, nil)
	require.NoError(t, err)
	require.True(t, perfect)
	require.Len(t, res, 1)
	require.Equal(t, 1, res[0].driverIndex)
	require.Equal(t, "builder-riscv64", res[0].Node().Builder)
}

func TestSelectNodeNonStrict(t *testing.T) {
	r := makeTestResolver(map[string][]ocispecs.Platform{
		"builder-amd64": {platforms.MustParse("linux/amd64")},
		"builder-arm64": {platforms.MustParse("linux/arm64")},
	})

	// arm64 should match itself
	res, perfect, err := r.resolve(context.TODO(), []ocispecs.Platform{platforms.MustParse("linux/arm64")}, nil, platforms.Only, nil)
	require.NoError(t, err)
	require.True(t, perfect)
	require.Len(t, res, 1)
	require.Equal(t, "builder-arm64", res[0].Node().Builder)

	// arm64 may support arm/v8
	res, perfect, err = r.resolve(context.TODO(), []ocispecs.Platform{platforms.MustParse("linux/arm/v8")}, nil, platforms.Only, nil)
	require.NoError(t, err)
	require.True(t, perfect)
	require.Len(t, res, 1)
	require.Equal(t, "builder-arm64", res[0].Node().Builder)

	// arm64 may support arm/v7
	res, perfect, err = r.resolve(context.TODO(), []ocispecs.Platform{platforms.MustParse("linux/arm/v7")}, nil, platforms.Only, nil)
	require.NoError(t, err)
	require.True(t, perfect)
	require.Len(t, res, 1)
	require.Equal(t, "builder-arm64", res[0].Node().Builder)
}

func TestSelectNodeNonStrictARM(t *testing.T) {
	r := makeTestResolver(map[string][]ocispecs.Platform{
		"builder-amd64": {platforms.MustParse("linux/amd64")},
		"builder-arm64": {platforms.MustParse("linux/arm64")},
		"builder-armv8": {platforms.MustParse("linux/arm/v8")},
	})

	res, perfect, err := r.resolve(context.TODO(), []ocispecs.Platform{platforms.MustParse("linux/arm/v8")}, nil, platforms.Only, nil)
	require.NoError(t, err)
	require.True(t, perfect)
	require.Len(t, res, 1)
	require.Equal(t, "builder-armv8", res[0].Node().Builder)

	res, perfect, err = r.resolve(context.TODO(), []ocispecs.Platform{platforms.MustParse("linux/arm/v7")}, nil, platforms.Only, nil)
	require.NoError(t, err)
	require.True(t, perfect)
	require.Len(t, res, 1)
	require.Equal(t, "builder-armv8", res[0].Node().Builder)
}

func TestSelectNodeNonStrictLower(t *testing.T) {
	r := makeTestResolver(map[string][]ocispecs.Platform{
		"builder-amd64": {platforms.MustParse("linux/amd64")},
		"builder-armv7": {platforms.MustParse("linux/arm/v7")},
	})

	// v8 can't be built on v7 (so we should select the default)...
	res, perfect, err := r.resolve(context.TODO(), []ocispecs.Platform{platforms.MustParse("linux/arm/v8")}, nil, platforms.Only, nil)
	require.NoError(t, err)
	require.False(t, perfect)
	require.Len(t, res, 1)
	require.Equal(t, "builder-amd64", res[0].Node().Builder)

	// ...but v6 can be built on v8
	res, perfect, err = r.resolve(context.TODO(), []ocispecs.Platform{platforms.MustParse("linux/arm/v6")}, nil, platforms.Only, nil)
	require.NoError(t, err)
	require.True(t, perfect)
	require.Len(t, res, 1)
	require.Equal(t, "builder-armv7", res[0].Node().Builder)
}

func TestSelectNodePreferStart(t *testing.T) {
	r := makeTestResolver(map[string][]ocispecs.Platform{
		"builder-amd64":     {platforms.MustParse("linux/amd64")},
		"builder-riscv64-1": {platforms.MustParse("linux/riscv64")},
		"builder-riscv64-2": {platforms.MustParse("linux/riscv64")},
	})

	res, perfect, err := r.resolve(context.TODO(), []ocispecs.Platform{platforms.MustParse("linux/riscv64")}, nil, platforms.Only, nil)
	require.NoError(t, err)
	require.True(t, perfect)
	require.Len(t, res, 1)
	require.Equal(t, "builder-riscv64-1", res[0].Node().Builder)
}

func TestSelectNodePreferExact(t *testing.T) {
	r := makeTestResolver(map[string][]ocispecs.Platform{
		"builder-armv8": {platforms.MustParse("linux/arm/v8")},
		"builder-armv7": {platforms.MustParse("linux/arm/v7")},
	})

	res, perfect, err := r.resolve(context.TODO(), []ocispecs.Platform{platforms.MustParse("linux/arm/v7")}, nil, platforms.Only, nil)
	require.NoError(t, err)
	require.True(t, perfect)
	require.Len(t, res, 1)
	require.Equal(t, "builder-armv7", res[0].Node().Builder)
}

func TestSelectNodeNoPlatform(t *testing.T) {
	r := makeTestResolver(map[string][]ocispecs.Platform{
		"builder-beta":    {platforms.MustParse("linux/beta")},
		"builder-default": {platforms.DefaultSpec()},
	})

	res, perfect, err := r.resolve(context.TODO(), []ocispecs.Platform{}, nil, platforms.Only, nil)
	require.NoError(t, err)
	require.True(t, perfect)
	require.Len(t, res, 1)
	require.Equal(t, "builder-beta", res[0].Node().Builder)
	require.Empty(t, res[0].Platforms())
}

func TestSelectNodeAdditionalPlatforms(t *testing.T) {
	r := makeTestResolver(map[string][]ocispecs.Platform{
		"builder-amd64": {platforms.MustParse("linux/amd64")},
		"builder-armv8": {platforms.MustParse("linux/arm/v8")},
	})

	res, perfect, err := r.resolve(context.TODO(), []ocispecs.Platform{platforms.MustParse("linux/arm/v7")}, nil, platforms.Only, nil)
	require.NoError(t, err)
	require.True(t, perfect)
	require.Len(t, res, 1)
	require.Equal(t, "builder-armv8", res[0].Node().Builder)

	res, perfect, err = r.resolve(context.TODO(), []ocispecs.Platform{platforms.MustParse("linux/arm/v7")}, nil, platforms.Only, func(idx int, n builder.Node) []ocispecs.Platform {
		if n.Builder == "builder-amd64" {
			return []ocispecs.Platform{platforms.MustParse("linux/arm/v7")}
		}
		return nil
	})
	require.NoError(t, err)
	require.True(t, perfect)
	require.Len(t, res, 1)
	require.Equal(t, "builder-amd64", res[0].Node().Builder)
}

func TestSplitNodeMultiPlatform(t *testing.T) {
	r := makeTestResolver(map[string][]ocispecs.Platform{
		"builder-amd64-arm64": {platforms.MustParse("linux/amd64"), platforms.MustParse("linux/arm64")},
		"builder-riscv64":     {platforms.MustParse("linux/riscv64")},
	})

	res, perfect, err := r.resolve(context.TODO(), []ocispecs.Platform{
		platforms.MustParse("linux/amd64"),
		platforms.MustParse("linux/arm64"),
	}, nil, platforms.Only, nil)
	require.NoError(t, err)
	require.True(t, perfect)
	require.Len(t, res, 1)
	require.Equal(t, "builder-amd64-arm64", res[0].Node().Builder)

	res, perfect, err = r.resolve(context.TODO(), []ocispecs.Platform{
		platforms.MustParse("linux/amd64"),
		platforms.MustParse("linux/riscv64"),
	}, nil, platforms.Only, nil)
	require.NoError(t, err)
	require.True(t, perfect)
	require.Len(t, res, 2)
	require.Equal(t, "builder-amd64-arm64", res[0].Node().Builder)
	require.Equal(t, "builder-riscv64", res[1].Node().Builder)
}

func TestSplitNodeMultiPlatformNoUnify(t *testing.T) {
	r := makeTestResolver(map[string][]ocispecs.Platform{
		"builder-amd64":         {platforms.MustParse("linux/amd64")},
		"builder-amd64-riscv64": {platforms.MustParse("linux/amd64"), platforms.MustParse("linux/riscv64")},
	})

	// the "best" choice would be the node with both platforms, but we're using
	// a naive algorithm that doesn't try to unify the platforms
	res, perfect, err := r.resolve(context.TODO(), []ocispecs.Platform{
		platforms.MustParse("linux/amd64"),
		platforms.MustParse("linux/riscv64"),
	}, nil, platforms.Only, nil)
	require.NoError(t, err)
	require.True(t, perfect)
	require.Len(t, res, 2)
	require.Equal(t, "builder-amd64", res[0].Node().Builder)
	require.Equal(t, "builder-amd64-riscv64", res[1].Node().Builder)
}

func makeTestResolver(nodes map[string][]ocispecs.Platform) *nodeResolver {
	var ns []builder.Node
	for name, platforms := range nodes {
		ns = append(ns, builder.Node{
			Builder:   name,
			Platforms: platforms,
		})
	}
	sort.Slice(ns, func(i, j int) bool {
		return ns[i].Builder < ns[j].Builder
	})
	return newDriverResolver(ns)
}
