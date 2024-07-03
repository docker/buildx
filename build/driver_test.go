package build

import (
	"context"
	"sort"
	"testing"

	"github.com/containerd/platforms"
	"github.com/docker/buildx/builder"
	specs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
)

func TestFindDriverSanity(t *testing.T) {
	r := makeTestResolver(map[string][]specs.Platform{
		"aaa": {platforms.DefaultSpec()},
	})

	res, perfect, err := r.resolve(context.TODO(), []specs.Platform{platforms.DefaultSpec()}, nil, platforms.OnlyStrict, nil)
	require.NoError(t, err)
	require.True(t, perfect)
	require.Len(t, res, 1)
	require.Equal(t, 0, res[0].driverIndex)
	require.Equal(t, "aaa", res[0].Node().Builder)
	require.Equal(t, []specs.Platform{platforms.DefaultSpec()}, res[0].platforms)
}

func TestFindDriverEmpty(t *testing.T) {
	r := makeTestResolver(nil)

	res, perfect, err := r.resolve(context.TODO(), []specs.Platform{platforms.DefaultSpec()}, nil, platforms.Only, nil)
	require.NoError(t, err)
	require.True(t, perfect)
	require.Nil(t, res)
}

func TestFindDriverWeirdName(t *testing.T) {
	r := makeTestResolver(map[string][]specs.Platform{
		"aaa": {platforms.MustParse("linux/amd64")},
		"bbb": {platforms.MustParse("linux/foobar")},
	})

	// find first platform
	res, perfect, err := r.resolve(context.TODO(), []specs.Platform{platforms.MustParse("linux/foobar")}, nil, platforms.Only, nil)
	require.NoError(t, err)
	require.True(t, perfect)
	require.Len(t, res, 1)
	require.Equal(t, 1, res[0].driverIndex)
	require.Equal(t, "bbb", res[0].Node().Builder)
}

func TestFindDriverUnknown(t *testing.T) {
	r := makeTestResolver(map[string][]specs.Platform{
		"aaa": {platforms.MustParse("linux/amd64")},
	})

	res, perfect, err := r.resolve(context.TODO(), []specs.Platform{platforms.MustParse("linux/riscv64")}, nil, platforms.Only, nil)
	require.NoError(t, err)
	require.False(t, perfect)
	require.Len(t, res, 1)
	require.Equal(t, 0, res[0].driverIndex)
	require.Equal(t, "aaa", res[0].Node().Builder)
}

func TestSelectNodeSinglePlatform(t *testing.T) {
	r := makeTestResolver(map[string][]specs.Platform{
		"aaa": {platforms.MustParse("linux/amd64")},
		"bbb": {platforms.MustParse("linux/riscv64")},
	})

	// find first platform
	res, perfect, err := r.resolve(context.TODO(), []specs.Platform{platforms.MustParse("linux/amd64")}, nil, platforms.Only, nil)
	require.NoError(t, err)
	require.True(t, perfect)
	require.Len(t, res, 1)
	require.Equal(t, 0, res[0].driverIndex)
	require.Equal(t, "aaa", res[0].Node().Builder)

	// find second platform
	res, perfect, err = r.resolve(context.TODO(), []specs.Platform{platforms.MustParse("linux/riscv64")}, nil, platforms.Only, nil)
	require.NoError(t, err)
	require.True(t, perfect)
	require.Len(t, res, 1)
	require.Equal(t, 1, res[0].driverIndex)
	require.Equal(t, "bbb", res[0].Node().Builder)

	// find an unknown platform, should match the first driver
	res, perfect, err = r.resolve(context.TODO(), []specs.Platform{platforms.MustParse("linux/s390x")}, nil, platforms.Only, nil)
	require.NoError(t, err)
	require.False(t, perfect)
	require.Len(t, res, 1)
	require.Equal(t, 0, res[0].driverIndex)
	require.Equal(t, "aaa", res[0].Node().Builder)
}

func TestSelectNodeMultiPlatform(t *testing.T) {
	r := makeTestResolver(map[string][]specs.Platform{
		"aaa": {platforms.MustParse("linux/amd64"), platforms.MustParse("linux/arm64")},
		"bbb": {platforms.MustParse("linux/riscv64")},
	})

	res, perfect, err := r.resolve(context.TODO(), []specs.Platform{platforms.MustParse("linux/amd64")}, nil, platforms.Only, nil)
	require.NoError(t, err)
	require.True(t, perfect)
	require.Len(t, res, 1)
	require.Equal(t, 0, res[0].driverIndex)
	require.Equal(t, "aaa", res[0].Node().Builder)

	res, perfect, err = r.resolve(context.TODO(), []specs.Platform{platforms.MustParse("linux/arm64")}, nil, platforms.Only, nil)
	require.NoError(t, err)
	require.True(t, perfect)
	require.Len(t, res, 1)
	require.Equal(t, 0, res[0].driverIndex)
	require.Equal(t, "aaa", res[0].Node().Builder)

	res, perfect, err = r.resolve(context.TODO(), []specs.Platform{platforms.MustParse("linux/riscv64")}, nil, platforms.Only, nil)
	require.NoError(t, err)
	require.True(t, perfect)
	require.Len(t, res, 1)
	require.Equal(t, 1, res[0].driverIndex)
	require.Equal(t, "bbb", res[0].Node().Builder)
}

func TestSelectNodeNonStrict(t *testing.T) {
	r := makeTestResolver(map[string][]specs.Platform{
		"aaa": {platforms.MustParse("linux/amd64")},
		"bbb": {platforms.MustParse("linux/arm64")},
	})

	// arm64 should match itself
	res, perfect, err := r.resolve(context.TODO(), []specs.Platform{platforms.MustParse("linux/arm64")}, nil, platforms.Only, nil)
	require.NoError(t, err)
	require.True(t, perfect)
	require.Len(t, res, 1)
	require.Equal(t, "bbb", res[0].Node().Builder)

	// arm64 may support arm/v8
	res, perfect, err = r.resolve(context.TODO(), []specs.Platform{platforms.MustParse("linux/arm/v8")}, nil, platforms.Only, nil)
	require.NoError(t, err)
	require.True(t, perfect)
	require.Len(t, res, 1)
	require.Equal(t, "bbb", res[0].Node().Builder)

	// arm64 may support arm/v7
	res, perfect, err = r.resolve(context.TODO(), []specs.Platform{platforms.MustParse("linux/arm/v7")}, nil, platforms.Only, nil)
	require.NoError(t, err)
	require.True(t, perfect)
	require.Len(t, res, 1)
	require.Equal(t, "bbb", res[0].Node().Builder)
}

func TestSelectNodeNonStrictARM(t *testing.T) {
	r := makeTestResolver(map[string][]specs.Platform{
		"aaa": {platforms.MustParse("linux/amd64")},
		"bbb": {platforms.MustParse("linux/arm64")},
		"ccc": {platforms.MustParse("linux/arm/v8")},
	})

	res, perfect, err := r.resolve(context.TODO(), []specs.Platform{platforms.MustParse("linux/arm/v8")}, nil, platforms.Only, nil)
	require.NoError(t, err)
	require.True(t, perfect)
	require.Len(t, res, 1)
	require.Equal(t, "ccc", res[0].Node().Builder)

	res, perfect, err = r.resolve(context.TODO(), []specs.Platform{platforms.MustParse("linux/arm/v7")}, nil, platforms.Only, nil)
	require.NoError(t, err)
	require.True(t, perfect)
	require.Len(t, res, 1)
	require.Equal(t, "ccc", res[0].Node().Builder)
}

func TestSelectNodeNonStrictLower(t *testing.T) {
	r := makeTestResolver(map[string][]specs.Platform{
		"aaa": {platforms.MustParse("linux/amd64")},
		"bbb": {platforms.MustParse("linux/arm/v7")},
	})

	// v8 can't be built on v7 (so we should select the default)...
	res, perfect, err := r.resolve(context.TODO(), []specs.Platform{platforms.MustParse("linux/arm/v8")}, nil, platforms.Only, nil)
	require.NoError(t, err)
	require.False(t, perfect)
	require.Len(t, res, 1)
	require.Equal(t, "aaa", res[0].Node().Builder)

	// ...but v6 can be built on v8
	res, perfect, err = r.resolve(context.TODO(), []specs.Platform{platforms.MustParse("linux/arm/v6")}, nil, platforms.Only, nil)
	require.NoError(t, err)
	require.True(t, perfect)
	require.Len(t, res, 1)
	require.Equal(t, "bbb", res[0].Node().Builder)
}

func TestSelectNodePreferStart(t *testing.T) {
	r := makeTestResolver(map[string][]specs.Platform{
		"aaa": {platforms.MustParse("linux/amd64")},
		"bbb": {platforms.MustParse("linux/riscv64")},
		"ccc": {platforms.MustParse("linux/riscv64")},
	})

	res, perfect, err := r.resolve(context.TODO(), []specs.Platform{platforms.MustParse("linux/riscv64")}, nil, platforms.Only, nil)
	require.NoError(t, err)
	require.True(t, perfect)
	require.Len(t, res, 1)
	require.Equal(t, "bbb", res[0].Node().Builder)
}

func TestSelectNodePreferExact(t *testing.T) {
	r := makeTestResolver(map[string][]specs.Platform{
		"aaa": {platforms.MustParse("linux/arm/v8")},
		"bbb": {platforms.MustParse("linux/arm/v7")},
	})

	res, perfect, err := r.resolve(context.TODO(), []specs.Platform{platforms.MustParse("linux/arm/v7")}, nil, platforms.Only, nil)
	require.NoError(t, err)
	require.True(t, perfect)
	require.Len(t, res, 1)
	require.Equal(t, "bbb", res[0].Node().Builder)
}

func TestSelectNodeNoPlatform(t *testing.T) {
	r := makeTestResolver(map[string][]specs.Platform{
		"aaa": {platforms.MustParse("linux/foobar")},
		"bbb": {platforms.DefaultSpec()},
	})

	res, perfect, err := r.resolve(context.TODO(), []specs.Platform{}, nil, platforms.Only, nil)
	require.NoError(t, err)
	require.True(t, perfect)
	require.Len(t, res, 1)
	require.Equal(t, "aaa", res[0].Node().Builder)
	require.Empty(t, res[0].platforms)
}

func TestSelectNodeAdditionalPlatforms(t *testing.T) {
	r := makeTestResolver(map[string][]specs.Platform{
		"aaa": {platforms.MustParse("linux/amd64")},
		"bbb": {platforms.MustParse("linux/arm/v8")},
	})

	res, perfect, err := r.resolve(context.TODO(), []specs.Platform{platforms.MustParse("linux/arm/v7")}, nil, platforms.Only, nil)
	require.NoError(t, err)
	require.True(t, perfect)
	require.Len(t, res, 1)
	require.Equal(t, "bbb", res[0].Node().Builder)

	res, perfect, err = r.resolve(context.TODO(), []specs.Platform{platforms.MustParse("linux/arm/v7")}, nil, platforms.Only, func(idx int, n builder.Node) []specs.Platform {
		if n.Builder == "aaa" {
			return []specs.Platform{platforms.MustParse("linux/arm/v7")}
		}
		return nil
	})
	require.NoError(t, err)
	require.True(t, perfect)
	require.Len(t, res, 1)
	require.Equal(t, "aaa", res[0].Node().Builder)
}

func TestSplitNodeMultiPlatform(t *testing.T) {
	r := makeTestResolver(map[string][]specs.Platform{
		"aaa": {platforms.MustParse("linux/amd64"), platforms.MustParse("linux/arm64")},
		"bbb": {platforms.MustParse("linux/riscv64")},
	})

	res, perfect, err := r.resolve(context.TODO(), []specs.Platform{
		platforms.MustParse("linux/amd64"),
		platforms.MustParse("linux/arm64"),
	}, nil, platforms.Only, nil)
	require.NoError(t, err)
	require.True(t, perfect)
	require.Len(t, res, 1)
	require.Equal(t, "aaa", res[0].Node().Builder)

	res, perfect, err = r.resolve(context.TODO(), []specs.Platform{
		platforms.MustParse("linux/amd64"),
		platforms.MustParse("linux/riscv64"),
	}, nil, platforms.Only, nil)
	require.NoError(t, err)
	require.True(t, perfect)
	require.Len(t, res, 2)
	require.Equal(t, "aaa", res[0].Node().Builder)
	require.Equal(t, "bbb", res[1].Node().Builder)
}

func TestSplitNodeMultiPlatformNoUnify(t *testing.T) {
	r := makeTestResolver(map[string][]specs.Platform{
		"aaa": {platforms.MustParse("linux/amd64")},
		"bbb": {platforms.MustParse("linux/amd64"), platforms.MustParse("linux/riscv64")},
	})

	// the "best" choice would be the node with both platforms, but we're using
	// a naive algorithm that doesn't try to unify the platforms
	res, perfect, err := r.resolve(context.TODO(), []specs.Platform{
		platforms.MustParse("linux/amd64"),
		platforms.MustParse("linux/riscv64"),
	}, nil, platforms.Only, nil)
	require.NoError(t, err)
	require.True(t, perfect)
	require.Len(t, res, 2)
	require.Equal(t, "aaa", res[0].Node().Builder)
	require.Equal(t, "bbb", res[1].Node().Builder)
}

func makeTestResolver(nodes map[string][]specs.Platform) *nodeResolver {
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
