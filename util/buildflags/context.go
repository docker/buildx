package buildflags

import (
	"strings"

	"github.com/distribution/reference"
	"github.com/pkg/errors"
)

func ParseContextNames(values []string) (map[string]string, error) {
	if len(values) == 0 {
		return nil, nil
	}

	result := make(map[string]string, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}

		kv := strings.SplitN(value, "=", 2)
		if len(kv) != 2 {
			return nil, errors.Errorf("invalid context value: %s, expected key=value", value)
		}
		name, err := NormalizeContextName(kv[0])
		if err != nil {
			return nil, err
		}
		result[name] = kv[1]
	}
	return result, nil
}

// NormalizeContextName returns the familiar form of a named build context.
func NormalizeContextName(name string) (string, error) {
	named, err := reference.ParseNormalizedNamed(name)
	if err != nil {
		return "", errors.Wrapf(err, "invalid context name %s", name)
	}
	return strings.TrimSuffix(reference.FamiliarString(named), ":latest"), nil
}
