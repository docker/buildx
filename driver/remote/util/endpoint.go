package remote

import (
	"net/url"

	"github.com/pkg/errors"
)

var schemes = map[string]struct{}{
	"tcp":              {},
	"unix":             {},
	"ssh":              {},
	"docker-container": {},
	"kube-pod":         {},
	"npipe":            {},
}

func IsValidEndpoint(ep string) error {
	endpoint, err := url.Parse(ep)
	if err != nil {
		return errors.Wrapf(err, "failed to parse endpoint %s", ep)
	}
	if _, ok := schemes[endpoint.Scheme]; !ok {
		return errors.Errorf("unrecognized url scheme %s", endpoint.Scheme)
	}
	return nil
}
