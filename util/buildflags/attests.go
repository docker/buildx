package buildflags

import (
	"encoding/csv"
	"fmt"
	"strconv"
	"strings"

	"github.com/pkg/errors"
)

func CanonicalizeAttest(attestType string, in string) string {
	if in == "" {
		return ""
	}
	if b, err := strconv.ParseBool(in); err == nil {
		return fmt.Sprintf("type=%s,disabled=%t", attestType, !b)
	}
	return fmt.Sprintf("type=%s,%s", attestType, in)
}

func ParseAttests(in []string) (map[string]*string, error) {
	out := map[string]*string{}
	for _, in := range in {
		in := in
		attestType, disabled, err := parseAttest(in)
		if err != nil {
			return nil, err
		}

		k := "attest:" + attestType
		if _, ok := out[k]; ok {
			return nil, errors.Errorf("duplicate attestation field %s", attestType)
		}
		if disabled {
			out[k] = nil
		} else {
			out[k] = &in
		}
	}
	return out, nil
}

func parseAttest(in string) (string, bool, error) {
	if in == "" {
		return "", false, nil
	}

	csvReader := csv.NewReader(strings.NewReader(in))
	fields, err := csvReader.Read()
	if err != nil {
		return "", false, err
	}

	attestType := ""
	disabled := true
	for _, field := range fields {
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			return "", false, errors.Errorf("invalid value %s", field)
		}
		key = strings.TrimSpace(strings.ToLower(key))

		switch key {
		case "type":
			attestType = value
		case "disabled":
			disabled, err = strconv.ParseBool(value)
			if err != nil {
				return "", false, err
			}
		}
	}
	if attestType == "" {
		return "", false, errors.Errorf("attestation type not specified")
	}

	return attestType, disabled, nil
}
