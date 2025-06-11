package imagetools

import (
	"encoding/base64"
	"encoding/json"

	"github.com/distribution/reference"
)

func toCredentialsFunc(a Auth) func(string) (string, string, error) {
	return func(host string) (string, string, error) {
		if host == "registry-1.docker.io" {
			host = "https://index.docker.io/v1/"
		}
		ac, err := a.GetAuthConfig(host)
		if err != nil {
			return "", "", err
		}
		if ac.IdentityToken != "" {
			return "", ac.IdentityToken, nil
		}
		return ac.Username, ac.Password, nil
	}
}

func RegistryAuthForRef(ref string, a Auth) (string, error) {
	if a == nil {
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
	ac, err := a.GetAuthConfig(host)
	if err != nil {
		return "", err
	}
	buf, err := json.Marshal(ac)
	if err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(buf), nil
}
