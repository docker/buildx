package bake

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/docker/buildx/build"
	"github.com/docker/buildx/controller/pb"
	"github.com/docker/buildx/util/osutil"
	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/util/entitlements"
	"github.com/stretchr/testify/require"
)

func TestEvaluateToExistingPath(t *testing.T) {
	tempDir, err := osutil.GetLongPathName(t.TempDir())
	require.NoError(t, err)

	// Setup temporary directory structure for testing
	existingFile := filepath.Join(tempDir, "existing_file")
	require.NoError(t, os.WriteFile(existingFile, []byte("test"), 0644))

	existingDir := filepath.Join(tempDir, "existing_dir")
	require.NoError(t, os.Mkdir(existingDir, 0755))

	symlinkToFile := filepath.Join(tempDir, "symlink_to_file")
	require.NoError(t, os.Symlink(existingFile, symlinkToFile))

	symlinkToDir := filepath.Join(tempDir, "symlink_to_dir")
	require.NoError(t, os.Symlink(existingDir, symlinkToDir))

	nonexistentPath := filepath.Join(tempDir, "nonexistent", "path", "file.txt")

	tests := []struct {
		name      string
		input     string
		expected  string
		expectErr bool
	}{
		{
			name:      "Existing file",
			input:     existingFile,
			expected:  existingFile,
			expectErr: false,
		},
		{
			name:      "Existing directory",
			input:     existingDir,
			expected:  existingDir,
			expectErr: false,
		},
		{
			name:      "Symlink to file",
			input:     symlinkToFile,
			expected:  existingFile,
			expectErr: false,
		},
		{
			name:      "Symlink to directory",
			input:     symlinkToDir,
			expected:  existingDir,
			expectErr: false,
		},
		{
			name:      "Non-existent path",
			input:     nonexistentPath,
			expected:  tempDir,
			expectErr: false,
		},
		{
			name:      "Non-existent intermediate path",
			input:     filepath.Join(tempDir, "nonexistent", "file.txt"),
			expected:  tempDir,
			expectErr: false,
		},
		{
			name:  "Root path",
			input: "/",
			expected: func() string {
				root, _ := filepath.Abs("/")
				return root
			}(),
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, _, err := evaluateToExistingPath(tt.input)

			if tt.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestDedupePaths(t *testing.T) {
	wd := osutil.GetWd()
	tcases := []struct {
		in  map[string]struct{}
		out map[string]struct{}
	}{
		{
			in: map[string]struct{}{
				"/a/b/c": {},
				"/a/b/d": {},
				"/a/b/e": {},
			},
			out: map[string]struct{}{
				"/a/b/c": {},
				"/a/b/d": {},
				"/a/b/e": {},
			},
		},
		{
			in: map[string]struct{}{
				"/a/b/c":      {},
				"/a/b/c/d":    {},
				"/a/b/c/d/e":  {},
				"/a/b/../b/c": {},
			},
			out: map[string]struct{}{
				"/a/b/c": {},
			},
		},
		{
			in: map[string]struct{}{
				filepath.Join(wd, "a/b/c"):    {},
				filepath.Join(wd, "../aa"):    {},
				filepath.Join(wd, "a/b"):      {},
				filepath.Join(wd, "a/b/d"):    {},
				filepath.Join(wd, "../aa/b"):  {},
				filepath.Join(wd, "../../bb"): {},
			},
			out: map[string]struct{}{
				"a/b":                         {},
				"../aa":                       {},
				filepath.Join(wd, "../../bb"): {},
			},
		},
	}

	for i, tc := range tcases {
		t.Run(fmt.Sprintf("case%d", i), func(t *testing.T) {
			out, err := dedupPaths(tc.in)
			if err != nil {
				require.NoError(t, err)
			}
			// convert to relative paths as that is shown to user
			arr := make([]string, 0, len(out))
			for k := range out {
				arr = append(arr, k)
			}
			require.NoError(t, err)
			arr = toRelativePaths(arr, wd)
			m := make(map[string]struct{})
			for _, v := range arr {
				m[filepath.ToSlash(v)] = struct{}{}
			}
			o := make(map[string]struct{}, len(tc.out))
			for k := range tc.out {
				o[filepath.ToSlash(k)] = struct{}{}
			}
			require.Equal(t, o, m)
		})
	}
}

func TestValidateEntitlements(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	// the paths returned by entitlements validation will have symlinks resolved
	expDir1, err := filepath.EvalSymlinks(dir1)
	require.NoError(t, err)
	expDir2, err := filepath.EvalSymlinks(dir2)
	require.NoError(t, err)

	escapeLink := filepath.Join(dir1, "escape_link")
	require.NoError(t, os.Symlink("../../aa", escapeLink))

	wd, err := os.Getwd()
	require.NoError(t, err)
	expWd, err := filepath.EvalSymlinks(wd)
	require.NoError(t, err)

	tcases := []struct {
		name     string
		conf     EntitlementConf
		opt      build.Options
		expected EntitlementConf
	}{
		{
			name: "No entitlements",
			opt: build.Options{
				Inputs: build.Inputs{
					ContextState: &llb.State{},
				},
			},
		},
		{
			name: "NetworkHostMissing",
			opt: build.Options{
				Allow: []entitlements.Entitlement{
					entitlements.EntitlementNetworkHost,
				},
			},
			expected: EntitlementConf{
				NetworkHost: true,
				FSRead:      []string{expWd},
			},
		},
		{
			name: "NetworkHostSet",
			conf: EntitlementConf{
				NetworkHost: true,
			},
			opt: build.Options{
				Allow: []entitlements.Entitlement{
					entitlements.EntitlementNetworkHost,
				},
			},
			expected: EntitlementConf{
				FSRead: []string{expWd},
			},
		},
		{
			name: "SecurityAndNetworkHostMissing",
			opt: build.Options{
				Allow: []entitlements.Entitlement{
					entitlements.EntitlementNetworkHost,
					entitlements.EntitlementSecurityInsecure,
				},
			},
			expected: EntitlementConf{
				NetworkHost:      true,
				SecurityInsecure: true,
				FSRead:           []string{expWd},
			},
		},
		{
			name: "SecurityMissingAndNetworkHostSet",
			conf: EntitlementConf{
				NetworkHost: true,
			},
			opt: build.Options{
				Allow: []entitlements.Entitlement{
					entitlements.EntitlementNetworkHost,
					entitlements.EntitlementSecurityInsecure,
				},
			},
			expected: EntitlementConf{
				SecurityInsecure: true,
				FSRead:           []string{expWd},
			},
		},
		{
			name: "SSHMissing",
			opt: build.Options{
				SSHSpecs: []*pb.SSH{
					{
						ID: "test",
					},
				},
			},
			expected: EntitlementConf{
				SSH:    true,
				FSRead: []string{expWd},
			},
		},
		{
			name: "ExportLocal",
			opt: build.Options{
				ExportsLocalPathsTemporary: []string{
					dir1,
					filepath.Join(dir1, "subdir"),
					dir2,
				},
			},
			expected: EntitlementConf{
				FSWrite: func() []string {
					exp := []string{expDir1, expDir2}
					slices.Sort(exp)
					return exp
				}(),
				FSRead: []string{expWd},
			},
		},
		{
			name: "SecretFromSubFile",
			opt: build.Options{
				SecretSpecs: []*pb.Secret{
					{
						FilePath: filepath.Join(dir1, "subfile"),
					},
				},
			},
			conf: EntitlementConf{
				FSRead: []string{wd, dir1},
			},
		},
		{
			name: "SecretFromEscapeLink",
			opt: build.Options{
				SecretSpecs: []*pb.Secret{
					{
						FilePath: escapeLink,
					},
				},
			},
			conf: EntitlementConf{
				FSRead: []string{wd, dir1},
			},
			expected: EntitlementConf{
				FSRead: []string{filepath.Join(expDir1, "../..")},
			},
		},
		{
			name: "SecretFromEscapeLinkAllowRoot",
			opt: build.Options{
				SecretSpecs: []*pb.Secret{
					{
						FilePath: escapeLink,
					},
				},
			},
			conf: EntitlementConf{
				FSRead: []string{"/"},
			},
			expected: EntitlementConf{
				FSRead: func() []string {
					// on windows root (/) is only allowed if it is the same volume as wd
					if filepath.VolumeName(wd) == filepath.VolumeName(escapeLink) {
						return nil
					}
					// if not, then escapeLink is not allowed
					exp, _, err := evaluateToExistingPath(escapeLink)
					require.NoError(t, err)
					exp, err = filepath.EvalSymlinks(exp)
					require.NoError(t, err)
					return []string{exp}
				}(),
			},
		},
		{
			name: "SecretFromEscapeLinkAllowAny",
			opt: build.Options{
				SecretSpecs: []*pb.Secret{
					{
						FilePath: escapeLink,
					},
				},
			},
			conf: EntitlementConf{
				FSRead: []string{"*"},
			},
			expected: EntitlementConf{},
		},
		{
			name: "NonExistingAllowedPathSubpath",
			opt: build.Options{
				ExportsLocalPathsTemporary: []string{
					dir1,
				},
			},
			conf: EntitlementConf{
				FSRead:  []string{wd},
				FSWrite: []string{filepath.Join(dir1, "not/exists")},
			},
			expected: EntitlementConf{
				FSWrite: []string{expDir1}, // dir1 is still needed as only subpath was allowed
			},
		},
		{
			name: "NonExistingAllowedPathMatches",
			opt: build.Options{
				ExportsLocalPathsTemporary: []string{
					filepath.Join(dir1, "not/exists"),
				},
			},
			conf: EntitlementConf{
				FSRead:  []string{wd},
				FSWrite: []string{filepath.Join(dir1, "not/exists")},
			},
			expected: EntitlementConf{
				FSWrite: []string{expDir1}, // dir1 is still needed as build also needs to write not/exists directory
			},
		},
		{
			name: "NonExistingBuildPath",
			opt: build.Options{
				ExportsLocalPathsTemporary: []string{
					filepath.Join(dir1, "not/exists"),
				},
			},
			conf: EntitlementConf{
				FSRead:  []string{wd},
				FSWrite: []string{dir1},
			},
		},
	}

	for _, tc := range tcases {
		t.Run(tc.name, func(t *testing.T) {
			expected, err := tc.conf.Validate(map[string]build.Options{"test": tc.opt})
			require.NoError(t, err)
			require.Equal(t, tc.expected, expected)
		})
	}
}

func TestGroupSamePaths(t *testing.T) {
	tests := []struct {
		name      string
		in1       []string
		in2       []string
		expected1 []string
		expected2 []string
		expectedC []string
	}{
		{
			name:      "All common paths",
			in1:       []string{"/path/a", "/path/b", "/path/c"},
			in2:       []string{"/path/a", "/path/b", "/path/c"},
			expected1: []string{},
			expected2: []string{},
			expectedC: []string{"/path/a", "/path/b", "/path/c"},
		},
		{
			name:      "No common paths",
			in1:       []string{"/path/a", "/path/b"},
			in2:       []string{"/path/c", "/path/d"},
			expected1: []string{"/path/a", "/path/b"},
			expected2: []string{"/path/c", "/path/d"},
			expectedC: []string{},
		},
		{
			name:      "Some common paths",
			in1:       []string{"/path/a", "/path/b", "/path/c"},
			in2:       []string{"/path/b", "/path/c", "/path/d"},
			expected1: []string{"/path/a"},
			expected2: []string{"/path/d"},
			expectedC: []string{"/path/b", "/path/c"},
		},
		{
			name:      "Empty inputs",
			in1:       []string{},
			in2:       []string{},
			expected1: []string{},
			expected2: []string{},
			expectedC: []string{},
		},
		{
			name:      "One empty input",
			in1:       []string{"/path/a", "/path/b"},
			in2:       []string{},
			expected1: []string{"/path/a", "/path/b"},
			expected2: []string{},
			expectedC: []string{},
		},
		{
			name:      "Unsorted inputs with common paths",
			in1:       []string{"/path/c", "/path/a", "/path/b"},
			in2:       []string{"/path/b", "/path/c", "/path/a"},
			expected1: []string{},
			expected2: []string{},
			expectedC: []string{"/path/a", "/path/b", "/path/c"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out1, out2, common := groupSamePaths(tt.in1, tt.in2)
			require.Equal(t, tt.expected1, out1, "in1 should match expected1")
			require.Equal(t, tt.expected2, out2, "in2 should match expected2")
			require.Equal(t, tt.expectedC, common, "common should match expectedC")
		})
	}
}
