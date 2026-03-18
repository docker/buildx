package imagetools

import "testing"

func TestIsWholeManifestTemplate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		format string
		match  bool
	}{
		{
			name:   "exact",
			format: "{{.Manifest}}",
			match:  true,
		},
		{
			name:   "trimmed-whitespace",
			format: "  {{.Manifest}}  ",
			match:  true,
		},
		{
			name:   "inner-whitespace",
			format: "{{ .Manifest }}",
			match:  true,
		},
		{
			name:   "not-whole-template",
			format: "{{.Manifest.Digest}}",
			match:  false,
		},
		{
			name:   "split-field-name-does-not-match",
			format: "{{.Mani   fest   }}",
			match:  false,
		},
		{
			name:   "extra-text",
			format: "{{.Manifest}}\n",
			match:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isWholeManifestTemplate(tc.format); got != tc.match {
				t.Fatalf("isWholeManifestTemplate(%q) = %v, want %v", tc.format, got, tc.match)
			}
		})
	}
}
