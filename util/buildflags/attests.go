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
			if disabled == nil {
				return nil, errors.Errorf("duplicate attestation field %s", attestType)
			}
			if *disabled {
				out[k] = nil
			}
			continue
		}

		if disabled != nil && *disabled {
			out[k] = nil
		} else {
			out[k] = &in
		}
	}
	return out, nil
}

func parseAttest(in string) (string, *bool, error) {
	if in == "" {
		return "", nil, nil
	}

	csvReader := csv.NewReader(strings.NewReader(in))
	fields, err := csvReader.Read()
	if err != nil {
		return "", nil, err
	}

	attestType := ""
	var disabled *bool
	for _, field := range fields {
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			return "", nil, errors.Errorf("invalid value %s", field)
		}
		key = strings.TrimSpace(strings.ToLower(key))

		switch key {
		case "type":
			attestType = value
		case "disabled":
			b, err := strconv.ParseBool(value)
			if err != nil {
				return "", nil, err
			}
			disabled = &b
		}
	}
	if attestType == "" {
		return "", nil, errors.Errorf("attestation type not specified")
	}

	return attestType, disabled, nil
}
