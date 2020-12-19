package bake

import (
	"os"
	"strings"

	"github.com/imdario/mergo"
	"github.com/joho/godotenv"
)

func readEnv() (envs map[string]string, err error) {
	envs, _ = godotenv.Read()
	err = mergo.Merge(&envs, envMap(os.Environ()), mergo.WithOverride)
	return
}

func envMap(env []string) map[string]string {
	result := make(map[string]string, len(env))
	for _, s := range env {
		kv := strings.SplitN(s, "=", 2)
		if len(kv) != 2 {
			continue
		}
		result[kv[0]] = kv[1]
	}
	return result
}
