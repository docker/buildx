package completion

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestPlatforms(t *testing.T) {
	values, directive := Platforms()(nil, nil, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("directive = %v, want NoFileComp", directive)
	}
	if len(values) == 0 {
		t.Fatal("expected platform completions")
	}
	want := map[string]bool{"linux/amd64": true, "linux/arm64": true, "windows/amd64": true}
	got := map[string]bool{}
	for _, v := range values {
		got[v] = true
	}
	for p := range want {
		if !got[p] {
			t.Errorf("missing platform %q in %v", p, values)
		}
	}
}
