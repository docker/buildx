package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/docker/buildx/util/confutil"
	"github.com/docker/cli/cli/command"
	"github.com/pkg/errors"
)

func configureProxy(cmd *command.DockerCli) error {
	fp := filepath.Join(confutil.ConfigDir(cmd), "proxy.json")
	dt, err := os.ReadFile(fp)
	if err != nil {
		if os.IsNotExist(err) || os.IsPermission(err) {
			return nil
		}
		return err
	}
	env := map[string]string{}
	if err := json.Unmarshal(dt, &env); err != nil {
		return errors.Wrapf(err, "failed to parse proxy config %s", fp)
	}
	permitted := map[string]struct{}{
		"HTTP_PROXY":  {},
		"HTTPS_PROXY": {},
		"NO_PROXY":    {},
		"FTP_PROXY":   {},
		"ALL_PROXY":   {},
	}
	for k := range permitted {
		if _, ok := os.LookupEnv(k); ok {
			continue
		}
		if val, ok := env[k]; ok {
			os.Setenv(k, val)
		}
	}
	return nil
}
