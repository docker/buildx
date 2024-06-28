package buildflags

import (
	"fmt"
	"strconv"
	"strings"

	controllerapi "github.com/docker/buildx/controller/pb"
	"github.com/pkg/errors"
	"github.com/tonistiigi/go-csvvalue"
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

func ParseAttests(in []string) ([]*controllerapi.Attest, error) {
	out := []*controllerapi.Attest{}
	found := map[string]struct{}{}
	for _, in := range in {
		in := in
		attest, err := ParseAttest(in)
		if err != nil {
			return nil, err
		}

		if _, ok := found[attest.Type]; ok {
			return nil, errors.Errorf("duplicate attestation field %s", attest.Type)
		}
		found[attest.Type] = struct{}{}

		out = append(out, attest)
	}
	return out, nil
}

func ParseAttest(in string) (*controllerapi.Attest, error) {
	if in == "" {
		return nil, nil
	}

	fields, err := csvvalue.Fields(in, nil)
	if err != nil {
		return nil, err
	}

	attest := controllerapi.Attest{
		Attrs: in,
	}
	for _, field := range fields {
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			return nil, errors.Errorf("invalid value %s", field)
		}
		key = strings.TrimSpace(strings.ToLower(key))

		switch key {
		case "type":
			attest.Type = value
		case "disabled":
			disabled, err := strconv.ParseBool(value)
			if err != nil {
				return nil, errors.Wrapf(err, "invalid value %s", field)
			}
			attest.Disabled = disabled
		}
	}
	if attest.Type == "" {
		return nil, errors.Errorf("attestation type not specified")
	}

	return &attest, nil
}
