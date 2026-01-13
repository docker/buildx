package buildflags

import (
	"os"
	"strconv"
	"strings"

	"github.com/docker/buildx/policy"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/tonistiigi/go-csvvalue"
)

type PolicyConfig struct {
	Files    []policy.File
	Reset    bool
	Disabled bool
	Strict   *bool
	LogLevel *logrus.Level
}

func ParsePolicyConfigs(in []string) ([]PolicyConfig, error) {
	if len(in) == 0 {
		return nil, nil
	}

	out := make([]PolicyConfig, 0, len(in))
	for _, s := range in {
		cfg, err := ParsePolicyConfig(s)
		if err != nil {
			return nil, err
		}
		out = append(out, cfg)
	}
	return out, nil
}

func ParsePolicyConfig(value string) (PolicyConfig, error) {
	fields, err := csvvalue.Fields(value, nil)
	if err != nil {
		return PolicyConfig{}, err
	}
	return parsePolicyFields(fields)
}

func parsePolicyFields(fields []string) (PolicyConfig, error) {
	cfg := PolicyConfig{}
	for _, field := range fields {
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			return PolicyConfig{}, errors.Errorf("invalid value %s", field)
		}
		key = strings.TrimSpace(strings.ToLower(key))
		switch key {
		case "filename":
			if value == "" {
				return PolicyConfig{}, errors.Errorf("invalid value %s", field)
			}
			dt, err := os.ReadFile(value)
			if err != nil {
				return PolicyConfig{}, errors.Wrapf(err, "failed to read policy file %s", value)
			}
			cfg.Files = append(cfg.Files, policy.File{Filename: value, Data: dt})
		case "reset":
			b, err := strconv.ParseBool(value)
			if err != nil {
				return PolicyConfig{}, errors.Wrapf(err, "invalid value %s", field)
			}
			cfg.Reset = b
		case "disabled":
			b, err := strconv.ParseBool(value)
			if err != nil {
				return PolicyConfig{}, errors.Wrapf(err, "invalid value %s", field)
			}
			cfg.Disabled = b
		case "strict":
			b, err := strconv.ParseBool(value)
			if err != nil {
				return PolicyConfig{}, errors.Wrapf(err, "invalid value %s", field)
			}
			cfg.Strict = &b
		case "log-level":
			lvl, err := logrus.ParseLevel(value)
			if err != nil {
				return PolicyConfig{}, errors.Wrapf(err, "invalid value %s", field)
			}
			cfg.LogLevel = &lvl
		default:
			return PolicyConfig{}, errors.Errorf("invalid value %s", field)
		}
	}
	return cfg, nil
}
