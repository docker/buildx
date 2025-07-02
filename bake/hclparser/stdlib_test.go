package hclparser

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zclconf/go-cty/cty"
)

func TestIndexOf(t *testing.T) {
	type testCase struct {
		input   cty.Value
		key     cty.Value
		want    cty.Value
		wantErr bool
	}
	tests := map[string]testCase{
		"index 0": {
			input: cty.TupleVal([]cty.Value{cty.StringVal("one"), cty.NumberIntVal(2.0), cty.NumberIntVal(3), cty.StringVal("four")}),
			key:   cty.StringVal("one"),
			want:  cty.NumberIntVal(0),
		},
		"index 3": {
			input: cty.TupleVal([]cty.Value{cty.StringVal("one"), cty.NumberIntVal(2.0), cty.NumberIntVal(3), cty.StringVal("four")}),
			key:   cty.StringVal("four"),
			want:  cty.NumberIntVal(3),
		},
		"index -1": {
			input:   cty.TupleVal([]cty.Value{cty.StringVal("one"), cty.NumberIntVal(2.0), cty.NumberIntVal(3), cty.StringVal("four")}),
			key:     cty.StringVal("3"),
			wantErr: true,
		},
	}

	for name, test := range tests {
		name, test := name, test
		t.Run(name, func(t *testing.T) {
			got, err := indexOfFunc().Call([]cty.Value{test.input, test.key})
			if test.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, test.want, got)
			}
		})
	}
}

func TestBasename(t *testing.T) {
	type testCase struct {
		input   cty.Value
		want    cty.Value
		wantErr bool
	}
	tests := map[string]testCase{
		"empty": {
			input: cty.StringVal(""),
			want:  cty.StringVal("."),
		},
		"slash": {
			input: cty.StringVal("/"),
			want:  cty.StringVal("/"),
		},
		"simple": {
			input: cty.StringVal("/foo/bar"),
			want:  cty.StringVal("bar"),
		},
		"simple no slash": {
			input: cty.StringVal("foo/bar"),
			want:  cty.StringVal("bar"),
		},
		"dot": {
			input: cty.StringVal("/foo/bar."),
			want:  cty.StringVal("bar."),
		},
		"dotdot": {
			input: cty.StringVal("/foo/bar.."),
			want:  cty.StringVal("bar.."),
		},
		"dotdotdot": {
			input: cty.StringVal("/foo/bar..."),
			want:  cty.StringVal("bar..."),
		},
	}

	for name, test := range tests {
		name, test := name, test
		t.Run(name, func(t *testing.T) {
			got, err := basenameFunc().Call([]cty.Value{test.input})
			if test.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, test.want, got)
			}
		})
	}
}

func TestDirname(t *testing.T) {
	type testCase struct {
		input   cty.Value
		want    cty.Value
		wantErr bool
	}
	tests := map[string]testCase{
		"empty": {
			input: cty.StringVal(""),
			want:  cty.StringVal("."),
		},
		"slash": {
			input: cty.StringVal("/"),
			want:  cty.StringVal("/"),
		},
		"simple": {
			input: cty.StringVal("/foo/bar"),
			want:  cty.StringVal("/foo"),
		},
		"simple no slash": {
			input: cty.StringVal("foo/bar"),
			want:  cty.StringVal("foo"),
		},
		"dot": {
			input: cty.StringVal("/foo/bar."),
			want:  cty.StringVal("/foo"),
		},
		"dotdot": {
			input: cty.StringVal("/foo/bar.."),
			want:  cty.StringVal("/foo"),
		},
		"dotdotdot": {
			input: cty.StringVal("/foo/bar..."),
			want:  cty.StringVal("/foo"),
		},
	}

	for name, test := range tests {
		name, test := name, test
		t.Run(name, func(t *testing.T) {
			got, err := dirnameFunc().Call([]cty.Value{test.input})
			if test.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, test.want, got)
			}
		})
	}
}

func TestSanitize(t *testing.T) {
	type testCase struct {
		input cty.Value
		want  cty.Value
	}
	tests := map[string]testCase{
		"empty": {
			input: cty.StringVal(""),
			want:  cty.StringVal(""),
		},
		"simple": {
			input: cty.StringVal("foo/bar"),
			want:  cty.StringVal("foo_bar"),
		},
		"simple no slash": {
			input: cty.StringVal("foobar"),
			want:  cty.StringVal("foobar"),
		},
		"dot": {
			input: cty.StringVal("foo/bar."),
			want:  cty.StringVal("foo_bar_"),
		},
		"dotdot": {
			input: cty.StringVal("foo/bar.."),
			want:  cty.StringVal("foo_bar__"),
		},
		"dotdotdot": {
			input: cty.StringVal("foo/bar..."),
			want:  cty.StringVal("foo_bar___"),
		},
		"utf8": {
			input: cty.StringVal("foo/🍕bar"),
			want:  cty.StringVal("foo__bar"),
		},
		"symbols": {
			input: cty.StringVal("foo/bar!@(ba+z)"),
			want:  cty.StringVal("foo_bar___ba_z_"),
		},
	}

	for name, test := range tests {
		name, test := name, test
		t.Run(name, func(t *testing.T) {
			got, err := sanitizeFunc().Call([]cty.Value{test.input})
			require.NoError(t, err)
			require.Equal(t, test.want, got)
		})
	}
}

func TestRfc3339ParseFunc(t *testing.T) {
	fn := rfc3339ParseFunc()
	input := cty.StringVal("2024-06-01T15:04:05Z")
	got, err := fn.Call([]cty.Value{input})
	require.NoError(t, err)

	expected := map[string]cty.Value{
		"year":         cty.NumberIntVal(2024),
		"year_day":     cty.NumberIntVal(153),
		"day":          cty.NumberIntVal(1),
		"month":        cty.NumberIntVal(6),
		"month_name":   cty.StringVal("June"),
		"weekday":      cty.NumberIntVal(6),
		"weekday_name": cty.StringVal("Saturday"),
		"hour":         cty.NumberIntVal(15),
		"minute":       cty.NumberIntVal(4),
		"second":       cty.NumberIntVal(5),
		"unix":         cty.NumberIntVal(1717254245),
		"iso_year":     cty.NumberIntVal(2024),
		"iso_week":     cty.NumberIntVal(22),
	}
	for k, v := range expected {
		require.True(t, got.GetAttr(k).RawEquals(v), "field %s: got %v, want %v", k, got.GetAttr(k), v)
	}

	_, err = fn.Call([]cty.Value{cty.StringVal("not-a-date")})
	require.Error(t, err)
}
