package commands

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestCompletionPlatforms(t *testing.T) {
	values, directive := completionPlatforms()(nil, nil, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("directive = %v, want NoFileComp", directive)
	}
	got := map[string]bool{}
	for _, v := range values {
		if got[v] {
			t.Errorf("duplicate platform %q", v)
		}
		got[v] = true
	}
	for _, p := range []string{"linux/amd64", "linux/arm64", "linux/arm/v7", "windows/amd64", "darwin/amd64"} {
		if !got[p] {
			t.Errorf("missing platform %q in %v", p, values)
		}
	}
}
