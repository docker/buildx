package buildflags

import (
	"cmp"
	"slices"
	"testing"

	gocmp "github.com/google/go-cmp/cmp"
	"github.com/moby/buildkit/exporter/containerimage/exptypes"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseAnnotations(t *testing.T) {
	tests := []struct {
		name    string
		in      []string
		want    map[exptypes.AnnotationKey]string
		wantErr string
	}{
		{
			name: "basic",
			in:   []string{"a=b"},
			want: map[exptypes.AnnotationKey]string{
				{Key: "a"}: "b",
			},
		},
		{
			name: "reverse-DNS key",
			in:   []string{"com.example=a"},
			want: map[exptypes.AnnotationKey]string{
				{Key: "com.example"}: "a",
			},
		},
		{
			name: "specify type",
			in:   []string{"manifest:com.example=a"},
			want: map[exptypes.AnnotationKey]string{
				{Type: "manifest", Key: "com.example"}: "a",
			},
		},
		{
			name:    "specify bad type",
			in:      []string{"bad:com.example=a"},
			wantErr: "unknown annotation type",
		},
		{
			name: "specify type and platform",
			in:   []string{"manifest[plat/form]:com.example=a"},
			want: map[exptypes.AnnotationKey]string{
				{
					Type: "manifest",
					Platform: &ocispecs.Platform{
						OS:           "plat",
						Architecture: "form",
					},
					Key: "com.example",
				}: "a",
			},
		},
		{
			name: "specify multiple types",
			in:   []string{"index,manifest:com.example=a"},
			want: map[exptypes.AnnotationKey]string{
				{Type: "index", Key: "com.example"}:    "a",
				{Type: "manifest", Key: "com.example"}: "a",
			},
		},
		{
			name: "specify multiple types and platform",
			in:   []string{"index,manifest[plat/form]:com.example=a"},
			want: map[exptypes.AnnotationKey]string{
				{Type: "index", Key: "com.example"}: "a",
				{
					Type: "manifest",
					Platform: &ocispecs.Platform{
						OS:           "plat",
						Architecture: "form",
					},
					Key: "com.example",
				}: "a",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, gotErr := ParseAnnotations(test.in)
			if test.wantErr != "" {
				require.ErrorContains(t, gotErr, test.wantErr)
			} else {
				assert.NoError(t, gotErr)
			}

			// Can't compare maps with pointer in their keys, need to extract and sort the map entries
			type kv struct {
				Key exptypes.AnnotationKey
				Val string
			}
			var wantKVs, gotKVs []kv
			for k, v := range test.want {
				wantKVs = append(wantKVs, kv{k, v})
			}
			for k, v := range got {
				gotKVs = append(gotKVs, kv{k, v})
			}

			sortFunc := func(a, b kv) int { return cmp.Compare(a.Key.String(), b.Key.String()) }
			slices.SortFunc(wantKVs, sortFunc)
			slices.SortFunc(gotKVs, sortFunc)

			if diff := gocmp.Diff(wantKVs, gotKVs); diff != "" {
				t.Error(diff)
			}
		})
	}
}
