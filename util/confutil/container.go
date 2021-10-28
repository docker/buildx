package confutil

import (
	"io"
	"os"
	"path"

	"github.com/pelletier/go-toml"
	"github.com/pkg/errors"
)

const (
	// DefaultBuildKitStateDir and DefaultBuildKitConfigDir are the location
	// where buildkitd inside the container stores its state. Some drivers
	// create a Linux container, so this should match the location for Linux,
	// as defined in: https://github.com/moby/buildkit/blob/v0.9.0/util/appdefaults/appdefaults_unix.go#L11-L15
	DefaultBuildKitStateDir  = "/var/lib/buildkit"
	DefaultBuildKitConfigDir = "/etc/buildkit"
)

// LoadConfigFiles creates a temp directory with BuildKit config and
// registry certificates ready to be copied to a container.
func LoadConfigFiles(bkconfig string) (string, error) {
	if _, err := os.Stat(bkconfig); errors.Is(err, os.ErrNotExist) {
		return "", errors.Wrapf(err, "buildkit configuration file not found: %s", bkconfig)
	} else if err != nil {
		return "", errors.Wrapf(err, "invalid buildkit configuration file: %s", bkconfig)
	}

	// Load config tree
	btoml, err := loadConfigTree(bkconfig)
	if err != nil {
		return "", err
	}

	// Temp dir that will be copied to the container
	tmpDir, err := os.MkdirTemp("", "buildkitd-config")
	if err != nil {
		return "", err
	}

	// Create BuildKit config folders
	tmpBuildKitConfigDir := path.Join(tmpDir, DefaultBuildKitConfigDir)
	tmpBuildKitCertsDir := path.Join(tmpBuildKitConfigDir, "certs")
	if err := os.MkdirAll(tmpBuildKitCertsDir, 0700); err != nil {
		return "", err
	}

	// Iterate through registry config to copy certs and update
	// BuildKit config with the underlying certs' path in the container.
	//
	// The following BuildKit config:
	//
	// [registry."myregistry.io"]
	//   ca=["/etc/config/myca.pem"]
	//   [[registry."myregistry.io".keypair]]
	//     key="/etc/config/key.pem"
	//     cert="/etc/config/cert.pem"
	//
	// will be translated in the container as:
	//
	// [registry."myregistry.io"]
	//   ca=["/etc/buildkit/certs/myregistry.io/myca.pem"]
	//   [[registry."myregistry.io".keypair]]
	//     key="/etc/buildkit/certs/myregistry.io/key.pem"
	//     cert="/etc/buildkit/certs/myregistry.io/cert.pem"
	if btoml.Has("registry") {
		for regName := range btoml.GetArray("registry").(*toml.Tree).Values() {
			regConf := btoml.GetPath([]string{"registry", regName}).(*toml.Tree)
			if regConf == nil {
				continue
			}
			regCertsDir := path.Join(tmpBuildKitCertsDir, regName)
			if err := os.Mkdir(regCertsDir, 0755); err != nil {
				return "", err
			}
			if regConf.Has("ca") {
				regCAs := regConf.GetArray("ca").([]string)
				if len(regCAs) > 0 {
					var cas []string
					for _, ca := range regCAs {
						cas = append(cas, path.Join(DefaultBuildKitConfigDir, "certs", regName, path.Base(ca)))
						if err := copyfile(ca, path.Join(regCertsDir, path.Base(ca))); err != nil {
							return "", err
						}
					}
					regConf.Set("ca", cas)
				}
			}
			if regConf.Has("keypair") {
				regKeyPairs := regConf.GetArray("keypair").([]*toml.Tree)
				if len(regKeyPairs) == 0 {
					continue
				}
				for _, kp := range regKeyPairs {
					if kp == nil {
						continue
					}
					key := kp.Get("key").(string)
					if len(key) > 0 {
						kp.Set("key", path.Join(DefaultBuildKitConfigDir, "certs", regName, path.Base(key)))
						if err := copyfile(key, path.Join(regCertsDir, path.Base(key))); err != nil {
							return "", err
						}
					}
					cert := kp.Get("cert").(string)
					if len(cert) > 0 {
						kp.Set("cert", path.Join(DefaultBuildKitConfigDir, "certs", regName, path.Base(cert)))
						if err := copyfile(cert, path.Join(regCertsDir, path.Base(cert))); err != nil {
							return "", err
						}
					}
				}
			}
		}
	}

	// Write BuildKit config
	bkfile, err := os.OpenFile(path.Join(tmpBuildKitConfigDir, "buildkitd.toml"), os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	_, err = btoml.WriteTo(bkfile)
	if err != nil {
		return "", err
	}

	return tmpDir, nil
}

func copyfile(src string, dst string) error {
	sf, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sf.Close()
	df, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer df.Close()
	_, err = io.Copy(df, sf)
	return err
}
