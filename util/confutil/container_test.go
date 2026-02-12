package confutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadConfigFilesMirrorOnlyPreservesTOML(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "buildkitd.toml")
	cfg := `[registry."docker.io"]
mirrors=["127.0.0.1:5000"]
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfg), 0644))

	m, err := LoadConfigFiles(cfgPath)
	require.NoError(t, err)

	got := string(m["buildkitd.toml"])
	require.Equal(t, `[registry]
[registry.'docker.io']
mirrors = ['127.0.0.1:5000']
`, got)
}

func TestLoadConfigFilesRewritesRegistryCertPaths(t *testing.T) {
	dir := t.TempDir()
	caPath := filepath.Join(dir, "myca.pem")
	keyPath := filepath.Join(dir, "mykey.pem")
	certPath := filepath.Join(dir, "mycert.pem")
	require.NoError(t, os.WriteFile(caPath, []byte("ca"), 0644))
	require.NoError(t, os.WriteFile(keyPath, []byte("key"), 0644))
	require.NoError(t, os.WriteFile(certPath, []byte("cert"), 0644))

	cfgPath := filepath.Join(dir, "buildkitd.toml")
	cfg := `[registry."myregistry.io"]
ca=['` + caPath + `']
[[registry."myregistry.io".keypair]]
key='` + keyPath + `'
cert='` + certPath + `'
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfg), 0644))

	m, err := LoadConfigFiles(cfgPath)
	require.NoError(t, err)

	got := string(m["buildkitd.toml"])
	require.Equal(t, `[registry]
[registry.'myregistry.io']
ca = ['/etc/buildkit/certs/myregistry.io/myca.pem']

[[registry.'myregistry.io'.keypair]]
cert = '/etc/buildkit/certs/myregistry.io/mycert.pem'
key = '/etc/buildkit/certs/myregistry.io/mykey.pem'
`, got)
	require.Equal(t, []byte("ca"), m["certs/myregistry.io/myca.pem"])
	require.Equal(t, []byte("key"), m["certs/myregistry.io/mykey.pem"])
	require.Equal(t, []byte("cert"), m["certs/myregistry.io/mycert.pem"])
}
