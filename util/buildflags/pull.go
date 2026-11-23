package buildflags

import (
	"encoding/json"
	"strconv"

	"github.com/pkg/errors"
)

type PullPolicy string

const (
	PullAlways  PullPolicy = "always"
	PullMissing PullPolicy = "missing"
	PullNever   PullPolicy = "never"
)

func (p *PullPolicy) Set(value string) error {
	switch PullPolicy(value) {
	case PullAlways, PullMissing, PullNever:
		*p = PullPolicy(value)
		return nil
	}
	if b, err := strconv.ParseBool(value); err == nil {
		// Retain boolean inputs for Bake serialization and expressions.
		*p = PullPolicy(strconv.FormatBool(b))
		return nil
	}
	return errors.Errorf("invalid pull policy %q: expected always, missing, never, true, or false", value)
}

func (p PullPolicy) String() string {
	if p == "" {
		return "false"
	}
	return string(p)
}

func (p PullPolicy) Type() string {
	return "string"
}

func (p PullPolicy) MarshalJSON() ([]byte, error) {
	switch p {
	case "", "false":
		return []byte("false"), nil
	case "true":
		return []byte("true"), nil
	default:
		return json.Marshal(string(p))
	}
}

func (p *PullPolicy) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return nil
	}
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		var b bool
		if err := json.Unmarshal(data, &b); err != nil {
			return err
		}
		value = strconv.FormatBool(b)
	}
	return p.Set(value)
}

func (p *PullPolicy) UnmarshalYAML(unmarshal func(any) error) error {
	var b bool
	if err := unmarshal(&b); err == nil {
		*p = PullPolicy(strconv.FormatBool(b))
		return nil
	}
	var value string
	if err := unmarshal(&value); err != nil {
		return err
	}
	return p.Set(value)
}
