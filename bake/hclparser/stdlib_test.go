package hclparser

import (
	"testing"

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
			got, err := indexOfFunc.Call([]cty.Value{test.input, test.key})
			if err != nil {
				if test.wantErr {
					return
				}
				t.Fatalf("unexpected error: %s", err)
			}
			if !got.RawEquals(test.want) {
				t.Errorf("wrong result\ngot:  %#v\nwant: %#v", got, test.want)
			}
		})
	}
}
