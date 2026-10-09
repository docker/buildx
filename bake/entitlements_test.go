package bake

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/docker/buildx/build"
	"github.com/docker/buildx/util/buildflags"
	"github.com/docker/buildx/util/osutil"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/util/entitlements"
	"github.com/stretchr/testify/require"
)

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
	credentialPath := filepath.Join(dir1, "credential")
	require.NoError(t, os.WriteFile(credentialPath, []byte("test credential"), 0600))
	expCredentialPath, err := filepath.EvalSymlinks(credentialPath)
	require.NoError(t, err)
	layoutLink := filepath.Join(dir1, "layout-link")
	require.NoError(t, os.Symlink(dir2, layoutLink))
	t.Setenv("BUILDX_TEST_SECRET_SOURCE", "test credential")

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
				Allow: []string{
					entitlements.EntitlementNetworkHost.String(),
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
				Allow: []string{
					entitlements.EntitlementNetworkHost.String(),
				},
			},
			expected: EntitlementConf{
				FSRead: []string{expWd},
			},
		},
		{
			name: "SecurityAndNetworkHostMissing",
			opt: build.Options{
				Allow: []string{
					entitlements.EntitlementNetworkHost.String(),
					entitlements.EntitlementSecurityInsecure.String(),
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
				Allow: []string{
					entitlements.EntitlementNetworkHost.String(),
					entitlements.EntitlementSecurityInsecure.String(),
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
				SSHSpecs: []*buildflags.SSH{
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
				SecretSpecs: []*buildflags.Secret{
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
			name: "secret-id-file-fallback-requires-read",
			opt: build.Options{
				SecretSpecs: []*buildflags.Secret{{ID: credentialPath}},
			},
			conf: EntitlementConf{FSRead: []string{wd}},
			expected: EntitlementConf{
				FSRead: []string{expCredentialPath},
			},
		},
		{
			name: "secret-id-file-fallback-allowed",
			opt: build.Options{
				SecretSpecs: []*buildflags.Secret{{ID: credentialPath}},
			},
			conf: EntitlementConf{FSRead: []string{wd, dir1}},
		},
		{
			name: "secret-id-env-fallback-needs-no-read",
			opt: build.Options{
				SecretSpecs: []*buildflags.Secret{{ID: "BUILDX_TEST_SECRET_SOURCE"}},
			},
			conf: EntitlementConf{FSRead: []string{wd}},
		},
		{
			name: "explicit-secret-env-needs-no-read",
			opt: build.Options{
				SecretSpecs: []*buildflags.Secret{{ID: credentialPath, Env: "BUILDX_TEST_SECRET_SOURCE"}},
			},
			conf: EntitlementConf{FSRead: []string{wd}},
		},
		{
			name: "oci-layout-named-context-requires-read",
			opt: build.Options{
				Inputs: build.Inputs{NamedContexts: map[string]build.NamedContext{
					"layout": {Path: "oci-layout://" + dir1 + ":latest"},
				}},
			},
			conf: EntitlementConf{FSRead: []string{wd}},
			expected: EntitlementConf{
				FSRead: []string{expDir1},
			},
		},
		{
			name: "oci-layout-named-context-allowed",
			opt: build.Options{
				Inputs: build.Inputs{NamedContexts: map[string]build.NamedContext{
					"layout": {Path: "oci-layout://" + dir1 + ":latest"},
				}},
			},
			conf: EntitlementConf{FSRead: []string{wd, dir1}},
		},
		{
			name: "oci-layout-named-context-symlink-requires-destination",
			opt: build.Options{
				Inputs: build.Inputs{NamedContexts: map[string]build.NamedContext{
					"layout": {Path: "oci-layout://" + layoutLink + ":latest"},
				}},
			},
			conf: EntitlementConf{FSRead: []string{wd, dir1}},
			expected: EntitlementConf{
				FSRead: []string{expDir2},
			},
		},
		{
			name: "SecretFromEscapeLink",
			opt: build.Options{
				SecretSpecs: []*buildflags.Secret{
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
				SecretSpecs: []*buildflags.Secret{
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
					exp, _, err := osutil.EvaluateToExistingPath(escapeLink)
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
				SecretSpecs: []*buildflags.Secret{
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
		{
			name: "LocalOutputDeleteMissing",
			conf: EntitlementConf{
				FSWrite: []string{"*"},
			},
			opt: build.Options{
				Inputs: build.Inputs{
					ContextState: &llb.State{},
				},
				Exports: []client.ExportEntry{
					{
						Type:      client.ExporterLocal,
						OutputDir: dir1,
						Attrs: map[string]string{
							"mode": string(client.LocalExporterModeDelete),
						},
					},
				},
				ExportsLocalPathsTemporary: []string{dir1},
			},
			expected: EntitlementConf{
				LocalOutputDelete: true,
			},
		},
		{
			name: "LocalOutputDeleteSet",
			conf: EntitlementConf{
				FSWrite:           []string{"*"},
				LocalOutputDelete: true,
			},
			opt: build.Options{
				Inputs: build.Inputs{
					ContextState: &llb.State{},
				},
				Exports: []client.ExportEntry{
					{
						Type:      client.ExporterLocal,
						OutputDir: dir1,
						Attrs: map[string]string{
							"mode": string(client.LocalExporterModeDelete),
						},
					},
				},
				ExportsLocalPathsTemporary: []string{dir1},
			},
		},
		{
			name: "LocalOutputCopy",
			conf: EntitlementConf{
				FSWrite: []string{"*"},
			},
			opt: build.Options{
				Inputs: build.Inputs{
					ContextState: &llb.State{},
				},
				Exports: []client.ExportEntry{
					{
						Type:      client.ExporterLocal,
						OutputDir: dir1,
						Attrs: map[string]string{
							"mode": string(client.LocalExporterModeCopy),
						},
					},
				},
				ExportsLocalPathsTemporary: []string{dir1},
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

func TestValidateEntitlementsMultiTarget(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	// the paths returned by entitlements validation will have symlinks resolved
	expDir2, err := filepath.EvalSymlinks(dir2)
	require.NoError(t, err)

	credentialPath := filepath.Join(dir1, "credential")
	require.NoError(t, os.WriteFile(credentialPath, []byte("test credential"), 0600))
	expCredentialPath, err := filepath.EvalSymlinks(credentialPath)
	require.NoError(t, err)

	tmpDir := t.TempDir()
	conf := EntitlementConf{
		FSRead:  []string{tmpDir},
		FSWrite: []string{tmpDir},
	}

	tcases := []struct {
		name     string
		targets  map[string]build.Options
		expected EntitlementConf
	}{
		{
			name: "Read",
			targets: map[string]build.Options{
				"secret": {
					SecretSpecs: []*buildflags.Secret{{FilePath: credentialPath}},
				},
			},
			expected: EntitlementConf{
				FSRead: []string{expCredentialPath},
			},
		},
		{
			name: "Write",
			targets: map[string]build.Options{
				"cache": {
					CacheTo: []client.CacheOptionsEntry{
						{Type: "local", Attrs: map[string]string{"dest": dir2}},
					},
				},
			},
			expected: EntitlementConf{
				FSWrite: []string{expDir2},
			},
		},
		{
			name: "DifferentPaths",
			targets: map[string]build.Options{
				"secret": {
					SecretSpecs: []*buildflags.Secret{{FilePath: credentialPath}},
				},
				"cache": {
					CacheFrom: []client.CacheOptionsEntry{
						{Type: "local", Attrs: map[string]string{"src": dir2}},
					},
				},
			},
			expected: EntitlementConf{
				FSRead: slices.Sorted(slices.Values([]string{expCredentialPath, expDir2})),
			},
		},
	}

	for _, tc := range tcases {
		t.Run(tc.name, func(t *testing.T) {
			// targets without missing paths must not mask the ones from other
			// targets, regardless of map iteration order
			for i := range 10 {
				tc.targets[fmt.Sprintf("clean%d", i)] = build.Options{}
			}
			for name, opt := range tc.targets {
				opt.Inputs = build.Inputs{ContextPath: tmpDir, DockerfilePath: filepath.Join(tmpDir, "Dockerfile")}
				tc.targets[name] = opt
			}
			for range 50 {
				expected, err := conf.Validate(tc.targets)
				require.NoError(t, err)
				require.Equal(t, tc.expected, expected)
			}
		})
	}
}

func TestValidateEntitlementsInvalidLocalOutputMode(t *testing.T) {
	_, err := EntitlementConf{}.Validate(map[string]build.Options{
		"test": {
			Inputs: build.Inputs{
				ContextState: &llb.State{},
			},
			Exports: []client.ExportEntry{
				{
					Type: client.ExporterLocal,
					Attrs: map[string]string{
						"mode": "backup",
					},
				},
			},
		},
	})
	require.ErrorContains(t, err, `invalid local exporter mode "backup"`)
}

func TestParseEntitlementsLocalOutputDelete(t *testing.T) {
	conf, err := ParseEntitlements([]string{string(EntitlementKeyBuildxLocalDelete)})
	require.NoError(t, err)
	require.True(t, conf.LocalOutputDelete)

	_, err = ParseEntitlements([]string{string(EntitlementKeyBuildxLocalDelete) + "=true"})
	require.ErrorContains(t, err, "buildx.local.delete does not accept a value")
}

func TestPromptLocalOutputDeleteCannotBeDisabledWithFSEntitlements(t *testing.T) {
	t.Setenv("BUILDX_BAKE_ENTITLEMENTS_FS", "0")

	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(nil)

	var out bytes.Buffer
	err := EntitlementConf{LocalOutputDelete: true}.Prompt(ctx, true, &out)
	require.ErrorContains(t, err, "additional privileges requested")
	require.Contains(t, out.String(), "Deleting stale files from local output destinations")
	require.Contains(t, out.String(), "--allow=buildx.local.delete")
}

func TestCheckEntitlements(t *testing.T) {
	t.Run("all entitlement types", func(t *testing.T) {
		t.Setenv("BUILDX_BAKE_ENTITLEMENTS_FS", "1")

		err := EntitlementConf{
			NetworkHost:      true,
			SecurityInsecure: true,
			Devices: &EntitlementsDevicesConf{
				Devices: map[string]struct{}{"vendor.com/device=foo": {}},
			},
			FSRead:            []string{t.TempDir()},
			FSWrite:           []string{t.TempDir()},
			SSH:               true,
			LocalOutputDelete: true,
		}.Check(false)
		require.Error(t, err)
		require.True(t, strings.HasPrefix(err.Error(), `additional privileges requested: pass "`), err.Error())
		for _, flag := range []string{
			"--allow=network.host",
			"--allow=security.insecure",
			"--allow=device=vendor.com/device=foo",
			"--allow=fs.read=",
			"--allow=fs.write=",
			"--allow=ssh",
			"--allow=buildx.local.delete",
		} {
			require.ErrorContains(t, err, flag)
		}
	})

	t.Run("filesystem checks disabled", func(t *testing.T) {
		t.Setenv("BUILDX_BAKE_ENTITLEMENTS_FS", "0")

		err := EntitlementConf{
			FSRead:  []string{t.TempDir()},
			FSWrite: []string{t.TempDir()},
			SSH:     true,
		}.Check(false)
		require.NoError(t, err)
	})

	t.Run("filesystem setting does not disable other checks", func(t *testing.T) {
		t.Setenv("BUILDX_BAKE_ENTITLEMENTS_FS", "0")

		err := EntitlementConf{
			NetworkHost: true,
			FSRead:      []string{t.TempDir()},
			SSH:         true,
		}.Check(false)
		require.ErrorContains(t, err, "--allow=network.host")
		require.NotContains(t, err.Error(), "--allow=fs.read=")
		require.NotContains(t, err.Error(), "--allow=ssh")
	})
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

func TestValidateEntitlementsPrefixSibling(t *testing.T) {
	root := t.TempDir()
	allowed := filepath.Join(root, "app")
	sibling := filepath.Join(root, "app-secrets")
	require.NoError(t, os.MkdirAll(filepath.Join(allowed, "sub"), 0755))
	require.NoError(t, os.MkdirAll(sibling, 0755))

	expSibling, err := filepath.EvalSymlinks(sibling)
	require.NoError(t, err)

	conf := EntitlementConf{
		FSRead:  []string{allowed},
		FSWrite: []string{allowed},
	}

	tcases := []struct {
		name     string
		opt      build.Options
		expected EntitlementConf
	}{
		{
			name: "ReadSibling",
			opt: build.Options{
				Inputs: build.Inputs{ContextPath: sibling, DockerfilePath: filepath.Join(sibling, "Dockerfile")},
			},
			expected: EntitlementConf{
				FSRead: []string{expSibling},
			},
		},
		{
			name: "WriteSibling",
			opt: build.Options{
				Inputs:                     build.Inputs{ContextPath: allowed, DockerfilePath: filepath.Join(allowed, "Dockerfile")},
				ExportsLocalPathsTemporary: []string{sibling},
			},
			expected: EntitlementConf{
				FSWrite: []string{expSibling},
			},
		},
		{
			name: "ReadWriteSubdir",
			opt: build.Options{
				Inputs:                     build.Inputs{ContextPath: filepath.Join(allowed, "sub"), DockerfilePath: filepath.Join(allowed, "sub", "Dockerfile")},
				ExportsLocalPathsTemporary: []string{filepath.Join(allowed, "sub")},
			},
		},
	}

	for _, tc := range tcases {
		t.Run(tc.name, func(t *testing.T) {
			expected, err := conf.Validate(map[string]build.Options{"test": tc.opt})
			require.NoError(t, err)
			require.Equal(t, tc.expected, expected)
		})
	}
}

func TestIsParentOrEqualPath(t *testing.T) {
	sep := string(filepath.Separator)
	root := sep
	if runtime.GOOS == "windows" {
		root = `C:\`
	}
	parent := filepath.Join(root, "a", "b")

	tcases := []struct {
		p      string
		parent string
		ok     bool
	}{
		{p: parent, parent: parent, ok: true},
		{p: filepath.Join(parent, "c"), parent: parent, ok: true},
		{p: filepath.Join(parent, "c", "d"), parent: parent, ok: true},
		{p: filepath.Join(parent, "c"), parent: parent + sep, ok: true},
		{p: parent + "c", parent: parent, ok: false},
		{p: parent + "-c", parent: parent, ok: false},
		{p: parent + ".bak", parent: parent, ok: false},
		{p: filepath.Join(root, "a"), parent: parent, ok: false},
		{p: filepath.Join(root, "a", "bc", "d"), parent: parent, ok: false},
		{p: parent, parent: root, ok: true},
	}
	if runtime.GOOS == "windows" {
		tcases = append(tcases, struct {
			p      string
			parent string
			ok     bool
		}{p: `D:\a`, parent: `C:\`, ok: false})
	}

	for _, tc := range tcases {
		t.Run(tc.p+"_"+tc.parent, func(t *testing.T) {
			require.Equal(t, tc.ok, isParentOrEqualPath(tc.p, tc.parent))
		})
	}
}
