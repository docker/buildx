package store

import (
	"os"
	"regexp"
	"strings"

	"github.com/moby/buildkit/identity"
	"github.com/pkg/errors"
)

var namePattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9\.\-_]*$`)

const generatedNamePrefix = "builder_"

var newID = identity.NewID

type errInvalidName struct {
	error
}

func (e *errInvalidName) Error() string {
	return e.error.Error()
}

func (e *errInvalidName) Unwrap() error {
	return e.error
}

func IsErrInvalidName(err error) bool {
	_, ok := err.(*errInvalidName)
	return ok
}

func ValidateName(s string) (string, error) {
	if !namePattern.MatchString(s) {
		return "", &errInvalidName{
			errors.Errorf("invalid name %s, name needs to start with a letter and may not contain symbols, except ._-", s),
		}
	}
	return strings.ToLower(s), nil
}

func GenerateName(txn *Txn) (string, error) {
	var name string
	for range 6 {
		name = generatedNamePrefix + newID()[:12]
		if _, err := txn.NodeGroupByName(name); err != nil {
			if !os.IsNotExist(errors.Cause(err)) {
				return "", err
			}
		} else {
			continue
		}
		return name, nil
	}
	return "", errors.Errorf("failed to generate random name")
}
