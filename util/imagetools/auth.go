package imagetools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"

	"github.com/containerd/containerd/v2/core/remotes/docker"
	"github.com/distribution/reference"
	"github.com/moby/buildkit/session/auth/authprovider"
)

func RegistryAuthForRef(ref string, auth authprovider.AuthConfigProvider) (string, error) {
	if auth == nil {
		return "", nil
	}
	r, err := parseRef(ref)
	if err != nil {
		return "", err
	}
	host := reference.Domain(r)
	if host == "docker.io" {
		host = "https://index.docker.io/v1/"
	}
	ac, err := auth(context.TODO(), host, nil, nil)
	if err != nil {
		return "", err
	}
	buf, err := json.Marshal(ac)
	if err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(buf), nil
}

type withBearerAuthorizer struct {
	docker.Authorizer
	AuthConfig authprovider.AuthConfigProvider
}

func (a *withBearerAuthorizer) Authorize(ctx context.Context, req *http.Request) error {
	ac, err := a.AuthConfig(ctx, req.Host, nil, nil)
	if err == nil && ac.RegistryToken != "" {
		req.Header.Set("Authorization", "Bearer "+ac.RegistryToken)
		return nil
	}
	return a.Authorizer.Authorize(ctx, req)
}
