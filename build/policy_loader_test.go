package build

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

func TestMemoizedPolicyFSRefCountedClose(t *testing.T) {
	var initCalls int
	var closeCalls int

	m := &memoizedPolicyFS{
		init: func() (fs.StatFS, func() error, error) {
			initCalls++
			root := fstest.MapFS{
				"policy.rego": &fstest.MapFile{Data: []byte("package docker\n")},
			}
			return root, func() error {
				closeCalls++
				return nil
			}, nil
		},
	}

	first, err := m.get()
	require.NoError(t, err)
	require.NotNil(t, first)
	require.Equal(t, 1, initCalls)

	second, err := m.get()
	require.NoError(t, err)
	require.NotNil(t, second)
	require.Equal(t, 1, initCalls)

	require.NoError(t, m.close())
	require.Equal(t, 0, closeCalls)

	third, err := m.get()
	require.NoError(t, err)
	require.NotNil(t, third)
	require.Equal(t, 1, initCalls)

	require.NoError(t, m.close())
	require.Equal(t, 0, closeCalls)

	require.NoError(t, m.close())
	require.Equal(t, 1, closeCalls)
}

func TestMemoizedPolicyFSReinitializesAfterAllRefsClosed(t *testing.T) {
	var initCalls int
	var closeCalls int

	m := &memoizedPolicyFS{
		init: func() (fs.StatFS, func() error, error) {
			initCalls++
			root := fstest.MapFS{
				"policy.rego": &fstest.MapFile{Data: []byte("package docker\n")},
			}
			return root, func() error {
				closeCalls++
				return nil
			}, nil
		},
	}

	first, err := m.get()
	require.NoError(t, err)
	require.NotNil(t, first)
	require.Equal(t, 1, initCalls)

	require.NoError(t, m.close())
	require.Equal(t, 1, closeCalls)

	second, err := m.get()
	require.NoError(t, err)
	require.NotNil(t, second)
	require.Equal(t, 2, initCalls)

	require.NoError(t, m.close())
	require.Equal(t, 2, closeCalls)
}

func TestLoadPolicyDataReleasesPolicyDir(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "policy.rego"), []byte("package docker\n"), 0600))

	provider := newPolicyPathFS(context.Background(), nil, policyOpt{
		ContextDir: dir,
	})

	_, ok, err := loadPolicyData(provider, "policy.rego")
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, os.RemoveAll(dir))
}

func TestPolicyPathFSRefClose(t *testing.T) {
	var initCalls int
	var closeCalls int

	p := &policyPathFS{}
	p.contextFS.init = func() (fs.StatFS, func() error, error) {
		initCalls++
		root := fstest.MapFS{
			"policy.rego": &fstest.MapFile{Data: []byte("package docker\n")},
		}
		return root, func() error {
			closeCalls++
			return nil
		}, nil
	}

	first := &policyPathFSRef{policyPathFS: p}
	_, err := first.Stat("policy.rego")
	require.NoError(t, err)
	f, err := first.Open("policy.rego")
	require.NoError(t, err)
	require.NoError(t, f.Close())
	require.Equal(t, 1, initCalls)

	require.NoError(t, first.Close())
	require.Equal(t, 1, closeCalls)

	shared := &policyPathFSRef{policyPathFS: p}
	_, err = shared.Stat("policy.rego")
	require.NoError(t, err)
	require.Equal(t, 2, initCalls)

	second := &policyPathFSRef{policyPathFS: p}
	_, err = second.Stat("policy.rego")
	require.NoError(t, err)
	require.Equal(t, 2, initCalls)

	require.NoError(t, second.Close())
	require.Equal(t, 1, closeCalls)

	require.NoError(t, shared.Close())
	require.Equal(t, 2, closeCalls)
}
