package hclparser

import (
	"path/filepath"
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
		t.Run(name, func(t *testing.T) {
			got, err := sanitizeFunc().Call([]cty.Value{test.input})
			require.NoError(t, err)
			require.Equal(t, test.want, got)
		})
	}
}

func TestHomedir(t *testing.T) {
	home, err := homedirFunc().Call(nil)
	require.NoError(t, err)
	require.NotEmpty(t, home.AsString())
	require.True(t, filepath.IsAbs(home.AsString()))
}

func TestSemverCmp(t *testing.T) {
	type testCase struct {
		version    cty.Value
		constraint cty.Value
		want       cty.Value
		wantErr    bool
	}
	tests := map[string]testCase{
		"valid constraint satisfied": {
			version:    cty.StringVal("1.2.3"),
			constraint: cty.StringVal(">= 1.0.0"),
			want:       cty.BoolVal(true),
		},
		"valid constraint not satisfied": {
			version:    cty.StringVal("2.1.0"),
			constraint: cty.StringVal("< 2.0.0"),
			want:       cty.BoolVal(false),
		},
		"valid constraint satisfied without patch": {
			version:    cty.StringVal("3.22"),
			constraint: cty.StringVal(">= 3.20"),
			want:       cty.BoolVal(true),
		},
		"invalid version": {
			version:    cty.StringVal("not-a-version"),
			constraint: cty.StringVal(">= 1.0.0"),
			wantErr:    true,
		},
		"invalid constraint": {
			version:    cty.StringVal("1.2.3"),
			constraint: cty.StringVal("not-a-constraint"),
			wantErr:    true,
		},
		"empty version": {
			version:    cty.StringVal(""),
			constraint: cty.StringVal(">= 1.0.0"),
			wantErr:    true,
		},
		"empty constraint": {
			version:    cty.StringVal("1.2.3"),
			constraint: cty.StringVal(""),
			wantErr:    true,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := semvercmpFunc().Call([]cty.Value{test.version, test.constraint})
			if test.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, test.want, got)
			}
		})
	}
}

func TestUnixTimestampParseFunc(t *testing.T) {
	type testCase struct {
		input   cty.Value
		want    map[string]cty.Value
		wantErr bool
	}
	tests := map[string]testCase{
		"positive timestamp": {
			input: cty.NumberIntVal(1690328596),
			want: map[string]cty.Value{
				"year":         cty.NumberIntVal(2023),
				"year_day":     cty.NumberIntVal(206),
				"day":          cty.NumberIntVal(25),
				"month":        cty.NumberIntVal(7),
				"month_name":   cty.StringVal("July"),
				"weekday":      cty.NumberIntVal(2),
				"weekday_name": cty.StringVal("Tuesday"),
				"hour":         cty.NumberIntVal(23),
				"minute":       cty.NumberIntVal(43),
				"second":       cty.NumberIntVal(16),
				"rfc3339":      cty.StringVal("2023-07-25T23:43:16Z"),
				"iso_year":     cty.NumberIntVal(2023),
				"iso_week":     cty.NumberIntVal(30),
			},
		},
		"zero timestamp": {
			input: cty.NumberIntVal(0),
			want: map[string]cty.Value{
				"year":         cty.NumberIntVal(1970),
				"year_day":     cty.NumberIntVal(1),
				"day":          cty.NumberIntVal(1),
				"month":        cty.NumberIntVal(1),
				"month_name":   cty.StringVal("January"),
				"weekday":      cty.NumberIntVal(4),
				"weekday_name": cty.StringVal("Thursday"),
				"hour":         cty.NumberIntVal(0),
				"minute":       cty.NumberIntVal(0),
				"second":       cty.NumberIntVal(0),
				"rfc3339":      cty.StringVal("1970-01-01T00:00:00Z"),
				"iso_year":     cty.NumberIntVal(1970),
				"iso_week":     cty.NumberIntVal(1),
			},
		},
		"negative timestamp": {
			input: cty.NumberIntVal(-1),
			want: map[string]cty.Value{
				"year":         cty.NumberIntVal(1969),
				"year_day":     cty.NumberIntVal(365),
				"day":          cty.NumberIntVal(31),
				"month":        cty.NumberIntVal(12),
				"month_name":   cty.StringVal("December"),
				"weekday":      cty.NumberIntVal(3),
				"weekday_name": cty.StringVal("Wednesday"),
				"hour":         cty.NumberIntVal(23),
				"minute":       cty.NumberIntVal(59),
				"second":       cty.NumberIntVal(59),
				"rfc3339":      cty.StringVal("1969-12-31T23:59:59Z"),
				"iso_year":     cty.NumberIntVal(1970),
				"iso_week":     cty.NumberIntVal(1),
			},
		},
		"fractional timestamp": {
			input:   cty.NumberFloatVal(1.2),
			wantErr: true,
		},
		"string timestamp": {
			input:   cty.StringVal("0"),
			wantErr: true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := unixtimestampParseFunc().Call([]cty.Value{test.input})
			if test.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			for k, v := range test.want {
				require.True(t, got.GetAttr(k).RawEquals(v), "field %s: got %v, want %v", k, got.GetAttr(k), v)
			}
		})
	}
}

func TestFormatTimestampFunc(t *testing.T) {
	type testCase struct {
		format  cty.Value
		input   cty.Value
		want    cty.Value
		wantErr bool
	}
	tests := map[string]testCase{
		"unix format from rfc3339 string": {
			format: cty.StringVal("X"),
			input:  cty.StringVal("2015-10-21T00:00:00Z"),
			want:   cty.StringVal("1445385600"),
		},
		"unix format from unix timestamp input": {
			format: cty.StringVal("X"),
			input:  cty.NumberIntVal(1445385600),
			want:   cty.StringVal("1445385600"),
		},
		"rfc3339 string input": {
			format: cty.StringVal("YYYY-MM-DD"),
			input:  cty.StringVal("2025-09-16T12:00:00Z"),
			want:   cty.StringVal("2025-09-16"),
		},
		"unix timestamp input": {
			format: cty.StringVal("YYYY-MM-DD'T'hh:mm:ssZ"),
			input:  cty.NumberIntVal(1690328596),
			want:   cty.StringVal("2023-07-25T23:43:16Z"),
		},
		"negative unix timestamp input": {
			format: cty.StringVal("YYYY-MM-DD'T'hh:mm:ssZ"),
			input:  cty.NumberIntVal(-1),
			want:   cty.StringVal("1969-12-31T23:59:59Z"),
		},
		"fractional unix timestamp input": {
			format:  cty.StringVal("YYYY-MM-DD"),
			input:   cty.NumberFloatVal(1.2),
			wantErr: true,
		},
		"invalid string input": {
			format:  cty.StringVal("YYYY-MM-DD"),
			input:   cty.StringVal("0"),
			wantErr: true,
		},
		"invalid string input for unix format": {
			format:  cty.StringVal("X"),
			input:   cty.StringVal("0"),
			wantErr: true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := formatTimestampFunc().Call([]cty.Value{test.format, test.input})
			if test.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, test.want, got)
			}
		})
	}
}
