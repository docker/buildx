package confutil

import (
	"io"
	"os"
	"path"
	"regexp"

	buildkitdconfig "github.com/moby/buildkit/cmd/buildkitd/config"
	"github.com/pelletier/go-toml/v2"
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

var reInvalidCertsDir = regexp.MustCompile(`[^a-zA-Z0-9.-]+`)

// LoadConfigFiles creates a temp directory with BuildKit config and
// registry certificates ready to be copied to a container.
func LoadConfigFiles(bkconfig string) (map[string][]byte, error) {
	if _, err := os.Stat(bkconfig); errors.Is(err, os.ErrNotExist) {
		return nil, errors.Wrapf(err, "buildkit configuration file not found: %s", bkconfig)
	} else if err != nil {
		return nil, errors.Wrapf(err, "invalid buildkit configuration file: %s", bkconfig)
	}

	cfg, err := buildkitdconfig.LoadFile(bkconfig)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to load buildkit configuration file: %s", bkconfig)
	}

	m := make(map[string][]byte)

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
	if cfg.Registries != nil {
		for regName, regConf := range cfg.Registries {
			pfx := path.Join("certs", reInvalidCertsDir.ReplaceAllString(regName, "_"))
			if regCAs := regConf.RootCAs; len(regCAs) > 0 {
				var cas []string
				for _, ca := range regCAs {
					fp := path.Join(pfx, path.Base(ca))
					cas = append(cas, path.Join(DefaultBuildKitConfigDir, fp))

					dt, err := readFile(ca)
					if err != nil {
						return nil, errors.Wrapf(err, "failed to read CA file: %s", ca)
					}
					m[fp] = dt
				}
				regConf.RootCAs = cas
			}
			if regKeyPairs := regConf.KeyPairs; len(regKeyPairs) > 0 {
				for i, kp := range regKeyPairs {
					key := kp.Key
					if len(key) > 0 {
						fp := path.Join(pfx, path.Base(key))
						kp.Key = path.Join(DefaultBuildKitConfigDir, fp)
						dt, err := readFile(key)
						if err != nil {
							return nil, errors.Wrapf(err, "failed to read key file: %s", key)
						}
						m[fp] = dt
					}
					cert := kp.Certificate
					if len(cert) > 0 {
						fp := path.Join(pfx, path.Base(cert))
						kp.Certificate = path.Join(DefaultBuildKitConfigDir, fp)
						dt, err := readFile(cert)
						if err != nil {
							return nil, errors.Wrapf(err, "failed to read cert file: %s", cert)
						}
						m[fp] = dt
					}
					regConf.KeyPairs[i] = kp
				}
			}
			cfg.Registries[regName] = regConf
		}
	}

	dt, err := toml.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	m["buildkitd.toml"] = dt

	return m, nil
}

func readFile(fp string) ([]byte, error) {
	sf, err := os.Open(fp)
	if err != nil {
		return nil, err
	}
	defer sf.Close()
	return io.ReadAll(io.LimitReader(sf, 1024*1024))
}
