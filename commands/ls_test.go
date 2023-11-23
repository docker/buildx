package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTruncPlatforms(t *testing.T) {
	tests := []struct {
		name         string
		platforms    []string
		max          int
		expectedList map[string][]string
		expectedOut  string
	}{
		{
			name:      "arm64 preferred and emulated",
			platforms: []string{"linux/arm64*", "linux/amd64", "linux/amd64/v2", "linux/riscv64", "linux/ppc64le", "linux/s390x", "linux/386", "linux/mips64le", "linux/mips64", "linux/arm/v7", "linux/arm/v6"},
			max:       4,
			expectedList: map[string][]string{
				"linux/amd64": {
					"linux/amd64",
					"linux/amd64/v2",
				},
				"linux/arm": {
					"linux/arm/v7",
					"linux/arm/v6",
				},
				"linux/arm64": {
					"linux/arm64*",
				},
				"linux/ppc64le": {
					"linux/ppc64le",
				},
			},
			expectedOut: "linux/amd64 (+2), linux/arm64*, linux/arm (+2), linux/ppc64le, (5 more)",
		},
		{
			name:      "riscv64 preferred only",
			platforms: []string{"linux/riscv64*"},
			max:       4,
			expectedList: map[string][]string{
				"linux/riscv64": {
					"linux/riscv64*",
				},
			},
			expectedOut: "linux/riscv64*",
		},
		{
			name:      "amd64 no preferred and emulated",
			platforms: []string{"linux/amd64", "linux/amd64/v2", "linux/amd64/v3", "linux/386", "linux/arm64", "linux/riscv64", "linux/ppc64le", "linux/s390x", "linux/mips64le", "linux/mips64", "linux/arm/v7", "linux/arm/v6"},
			max:       4,
			expectedList: map[string][]string{
				"linux/amd64": {
					"linux/amd64",
					"linux/amd64/v2",
					"linux/amd64/v3",
				},
				"linux/arm": {
					"linux/arm/v7",
					"linux/arm/v6",
				},
				"linux/arm64": {
					"linux/arm64",
				},
				"linux/ppc64le": {
					"linux/ppc64le",
				}},
			expectedOut: "linux/amd64 (+3), linux/arm64, linux/arm (+2), linux/ppc64le, (5 more)",
		},
		{
			name:      "amd64 no preferred",
			platforms: []string{"linux/amd64", "linux/386"},
			max:       4,
			expectedList: map[string][]string{
				"linux/386": {
					"linux/386",
				},
				"linux/amd64": {
					"linux/amd64",
				},
			},
			expectedOut: "linux/amd64, linux/386",
		},
		{
			name:      "arm64 no preferred",
			platforms: []string{"linux/arm64", "linux/arm/v7", "linux/arm/v6"},
			max:       4,
			expectedList: map[string][]string{
				"linux/arm": {
					"linux/arm/v7",
					"linux/arm/v6",
				},
				"linux/arm64": {
					"linux/arm64",
				},
			},
			expectedOut: "linux/arm64, linux/arm (+2)",
		},
		{
			name:      "all preferred",
			platforms: []string{"darwin/arm64*", "linux/arm64*", "linux/arm/v5*", "linux/arm/v6*", "linux/arm/v7*", "windows/arm64*"},
			max:       4,
			expectedList: map[string][]string{
				"darwin/arm64": {
					"darwin/arm64*",
				},
				"linux/arm": {
					"linux/arm/v5*",
					"linux/arm/v6*",
					"linux/arm/v7*",
				},
				"linux/arm64": {
					"linux/arm64*",
				},
				"windows/arm64": {
					"windows/arm64*",
				},
			},
			expectedOut: "linux/arm64*, linux/arm* (+3), darwin/arm64*, windows/arm64*",
		},
		{
			name:      "no major preferred",
			platforms: []string{"linux/amd64/v2*", "linux/arm/v6*", "linux/mips64le*", "linux/amd64", "linux/amd64/v3", "linux/386", "linux/arm64", "linux/riscv64", "linux/ppc64le", "linux/s390x", "linux/mips64", "linux/arm/v7"},
			max:       4,
			expectedList: map[string][]string{
				"linux/amd64": {
					"linux/amd64/v2*",
					"linux/amd64",
					"linux/amd64/v3",
				},
				"linux/arm": {
					"linux/arm/v6*",
					"linux/arm/v7",
				},
				"linux/arm64": {
					"linux/arm64",
				},
				"linux/ppc64le": {
					"linux/ppc64le",
				},
			},
			expectedOut: "linux/amd64* (+3), linux/arm64, linux/arm* (+2), linux/ppc64le, (5 more)",
		},
		{
			name:      "no major with multiple variants",
			platforms: []string{"linux/arm64", "linux/arm/v7", "linux/arm/v6", "linux/mips64le/softfloat", "linux/mips64le/hardfloat"},
			max:       4,
			expectedList: map[string][]string{
				"linux/arm": {
					"linux/arm/v7",
					"linux/arm/v6",
				},
				"linux/arm64": {
					"linux/arm64",
				},
				"linux/mips64le": {
					"linux/mips64le/softfloat",
					"linux/mips64le/hardfloat",
				},
			},
			expectedOut: "linux/arm64, linux/arm (+2), linux/mips64le (+2)",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			tpfs := truncPlatforms(tt.platforms, tt.max)
			assert.Equal(t, tt.expectedList, tpfs.List())
			assert.Equal(t, tt.expectedOut, tpfs.String())
		})
	}
}
