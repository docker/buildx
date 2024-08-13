package buildflags

import (
	"strconv"
	"strings"

	controllerapi "github.com/docker/buildx/controller/pb"
	"github.com/pkg/errors"
	"github.com/tonistiigi/go-csvvalue"
)

const defaultCallFunc = "build"

func ParseCallFunc(str string) (*controllerapi.CallFunc, error) {
	if str == "" {
		return nil, nil
	}

	fields, err := csvvalue.Fields(str, nil)
	if err != nil {
		return nil, err
	}
	f := &controllerapi.CallFunc{}
	for _, field := range fields {
		parts := strings.SplitN(field, "=", 2)
		if len(parts) == 2 {
			switch parts[0] {
			case "format":
				f.Format = parts[1]
			case "ignorestatus":
				v, err := strconv.ParseBool(parts[1])
				if err != nil {
					return nil, errors.Wrapf(err, "invalid ignorestatus print value: %s", parts[1])
				}
				f.IgnoreStatus = v
			default:
				return nil, errors.Errorf("invalid print field: %s", field)
			}
		} else {
			if f.Name != "" {
				return nil, errors.Errorf("invalid print value: %s", str)
			}
			f.Name = field
		}
	}

	// "check" has been added as an alias for "lint",
	// in order to maintain backwards compatibility
	// we need to convert it.
	if f.Name == "check" {
		f.Name = "lint"
	}

	if f.Name == defaultCallFunc {
		return nil, nil
	}

	return f, nil
}
