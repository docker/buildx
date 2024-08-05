package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTruncPlatforms(t *testing.T) {
	tests := []struct {
		name      string
		platforms []string
		expected  []string
		max       int
	}{
		{
			name:      "arm64 preferred and emulated",
			platforms: []string{"linux/arm64*", "linux/amd64", "linux/amd64/v2", "linux/riscv64", "linux/ppc64le", "linux/s390x", "linux/386", "linux/mips64le", "linux/mips64", "linux/arm/v7", "linux/arm/v6"},
			expected:  []string{"linux/arm64*", "linux/amd64", "linux/arm/v7", "linux/ppc64le"},
			max:       4,
		},
		{
			name:      "riscv64 preferred only",
			platforms: []string{"linux/riscv64*"},
			expected:  []string{"linux/riscv64*"},
			max:       4,
		},
		{
			name:      "amd64 no preferred and emulated",
			platforms: []string{"linux/amd64", "linux/amd64/v2", "linux/amd64/v3", "linux/386", "linux/arm64", "linux/riscv64", "linux/ppc64le", "linux/s390x", "linux/mips64le", "linux/mips64", "linux/arm/v7", "linux/arm/v6"},
			expected:  []string{"linux/amd64", "linux/arm64", "linux/arm/v7", "linux/ppc64le"},
			max:       4,
		},
		{
			name:      "amd64 no preferred",
			platforms: []string{"linux/amd64", "linux/386"},
			expected:  []string{"linux/amd64", "linux/386"},
			max:       4,
		},
		{
			name:      "arm64 no preferred",
			platforms: []string{"linux/arm64", "linux/arm/v7", "linux/arm/v6"},
			expected:  []string{"linux/arm64", "linux/arm/v7", "linux/arm/v6"},
			max:       4,
		},
		{
			name:      "all preferred",
			platforms: []string{"darwin/arm64*", "linux/arm64*", "linux/arm/v5*", "linux/arm/v6*", "linux/arm/v7*", "windows/arm64*"},
			expected:  []string{"linux/arm64*", "linux/arm/v7*", "darwin/arm64*", "linux/arm/v5*"},
			max:       4,
		},
		{
			name:      "no major preferred",
			platforms: []string{"linux/amd64/v2*", "linux/arm/v6*", "linux/mips64le*", "linux/amd64", "linux/amd64/v3", "linux/386", "linux/arm64", "linux/riscv64", "linux/ppc64le", "linux/s390x", "linux/mips64", "linux/arm/v7"},
			expected:  []string{"linux/amd64", "linux/arm64", "linux/arm/v7", "linux/ppc64le"},
			max:       4,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, truncPlatforms(tt.platforms, tt.max))
		})
	}
}
