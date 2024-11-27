package buildflags

import (
	"strings"

	controllerapi "github.com/docker/buildx/controller/pb"
	"github.com/pkg/errors"
	"github.com/tonistiigi/go-csvvalue"
)

func ParseSecretSpecs(sl []string) ([]*controllerapi.Secret, error) {
	fs := make([]*controllerapi.Secret, 0, len(sl))
	for _, v := range sl {
		s, err := parseSecret(v)
		if err != nil {
			return nil, err
		}
		fs = append(fs, s)
	}
	return fs, nil
}

func parseSecret(value string) (*controllerapi.Secret, error) {
	fields, err := csvvalue.Fields(value, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse csv secret")
	}

	fs := controllerapi.Secret{}

	var typ string
	for _, field := range fields {
		parts := strings.SplitN(field, "=", 2)
		key := strings.ToLower(parts[0])

		if len(parts) != 2 {
			return nil, errors.Errorf("invalid field '%s' must be a key=value pair", field)
		}

		value := parts[1]
		switch key {
		case "type":
			if value != "file" && value != "env" {
				return nil, errors.Errorf("unsupported secret type %q", value)
			}
			typ = value
		case "id":
			fs.ID = value
		case "source", "src":
			fs.FilePath = value
		case "env":
			fs.Env = value
		default:
			return nil, errors.Errorf("unexpected key '%s' in '%s'", key, field)
		}
	}
	if typ == "env" && fs.Env == "" {
		fs.Env = fs.FilePath
		fs.FilePath = ""
	}
	return &fs, nil
}
