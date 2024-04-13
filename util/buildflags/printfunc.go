package buildflags

import (
	"encoding/csv"
	"strconv"
	"strings"

	controllerapi "github.com/docker/buildx/controller/pb"
	"github.com/pkg/errors"
)

func ParsePrintFunc(str string) (*controllerapi.PrintFunc, error) {
	if str == "" {
		return nil, nil
	}
	csvReader := csv.NewReader(strings.NewReader(str))
	fields, err := csvReader.Read()
	if err != nil {
		return nil, err
	}
	f := &controllerapi.PrintFunc{}
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
	return f, nil
}
