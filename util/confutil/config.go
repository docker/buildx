package confutil

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/docker/cli/cli/command"
	"github.com/docker/docker/pkg/ioutils"
	"github.com/pelletier/go-toml"
	"github.com/pkg/errors"
)

var (
	sudoerUID = -1
	sudoerGID = -1
)

type Config struct {
	dir string
}

type ConfigOption func(*configOptions)

type configOptions struct {
	dir string
}

func WithDir(dir string) ConfigOption {
	return func(o *configOptions) {
		o.dir = dir
	}
}

func NewConfig(dockerCli command.Cli, opts ...ConfigOption) *Config {
	co := configOptions{}
	for _, opt := range opts {
		opt(&co)
	}
	configDir := co.dir
	if configDir == "" {
		configDir = os.Getenv("BUILDX_CONFIG")
		if configDir == "" {
			configDir = filepath.Join(filepath.Dir(dockerCli.ConfigFile().Filename), "buildx")
		}
	}
	return &Config{
		dir: configDir,
	}
}

// Dir will look for correct configuration store path;
// if `$BUILDX_CONFIG` is set - use it, otherwise use parent directory
// of Docker config file (i.e. `${DOCKER_CONFIG}/buildx`)
func (c *Config) Dir() string {
	return c.dir
}

// BuildKitConfigFile returns the default BuildKit configuration file path
func (c *Config) BuildKitConfigFile() (string, bool) {
	f := filepath.Join(c.dir, "buildkitd.default.toml")
	if _, err := os.Stat(f); err == nil {
		return f, true
	}
	return "", false
}

// MkdirAll creates a directory and all necessary parents within the config dir
func (c *Config) MkdirAll(dir string, perm os.FileMode) error {
	d := filepath.Join(c.dir, dir)
	if err := os.MkdirAll(d, perm); err != nil {
		return err
	}
	if sudoerUID != -1 && sudoerGID != -1 {
		// apply chown to each directory level
		parts := strings.Split(dir, string(filepath.Separator))
		for i := range parts {
			if err := os.Chown(filepath.Join(c.dir, filepath.Join(parts[:i+1]...)), sudoerUID, sudoerGID); err != nil {
				return err
			}
		}
	}
	return nil
}

// WriteFile writes data to a file within the config dir
func (c *Config) WriteFile(filename string, data []byte, perm os.FileMode) error {
	f := filepath.Join(c.dir, filename)
	if err := os.WriteFile(f, data, perm); err != nil {
		return err
	}
	if sudoerUID != -1 && sudoerGID != -1 {
		return os.Chown(f, sudoerUID, sudoerGID)
	}
	return nil
}

// AtomicWriteFile writes data to a file within the config dir atomically
func (c *Config) AtomicWriteFile(filename string, data []byte, perm os.FileMode) error {
	f := filepath.Join(c.dir, filename)
	if err := ioutils.AtomicWriteFile(f, data, perm); err != nil {
		return err
	}
	if sudoerUID != -1 && sudoerGID != -1 {
		return os.Chown(f, sudoerUID, sudoerGID)
	}
	return nil
}

var nodeIdentifierMu sync.Mutex

func (c *Config) TryNodeIdentifier() (out string) {
	nodeIdentifierMu.Lock()
	defer nodeIdentifierMu.Unlock()
	sessionFilename := ".buildNodeID"
	sessionFilepath := filepath.Join(c.Dir(), sessionFilename)
	if _, err := os.Lstat(sessionFilepath); err != nil {
		if os.IsNotExist(err) { // create a new file with stored randomness
			b := make([]byte, 8)
			if _, err := rand.Read(b); err != nil {
				return out
			}
			if err := c.WriteFile(sessionFilename, []byte(hex.EncodeToString(b)), 0600); err != nil {
				return out
			}
		}
	}
	dt, err := os.ReadFile(sessionFilepath)
	if err == nil {
		return string(dt)
	}
	return
}

// LoadConfigTree loads BuildKit config toml tree
func LoadConfigTree(fp string) (*toml.Tree, error) {
	f, err := os.Open(fp)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, errors.Wrapf(err, "failed to load config from %s", fp)
	}
	defer f.Close()
	t, err := toml.LoadReader(f)
	if err != nil {
		return t, errors.Wrap(err, "failed to parse config")
	}
	return t, nil
}
