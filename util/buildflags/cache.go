package buildflags

import (
	"context"
	"os"
	"strings"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	controllerapi "github.com/docker/buildx/controller/pb"
	"github.com/pkg/errors"
	"github.com/tonistiigi/go-csvvalue"
)

func ParseCacheEntry(in []string) ([]*controllerapi.CacheOptionsEntry, error) {
	outs := make([]*controllerapi.CacheOptionsEntry, 0, len(in))
	for _, in := range in {
		fields, err := csvvalue.Fields(in, nil)
		if err != nil {
			return nil, err
		}
		if isRefOnlyFormat(fields) {
			for _, field := range fields {
				outs = append(outs, &controllerapi.CacheOptionsEntry{
					Type:  "registry",
					Attrs: map[string]string{"ref": field},
				})
			}
			continue
		}

		out := controllerapi.CacheOptionsEntry{
			Attrs: map[string]string{},
		}
		for _, field := range fields {
			parts := strings.SplitN(field, "=", 2)
			if len(parts) != 2 {
				return nil, errors.Errorf("invalid value %s", field)
			}
			key := strings.ToLower(parts[0])
			value := parts[1]
			switch key {
			case "type":
				out.Type = value
			default:
				out.Attrs[key] = value
			}
		}
		if out.Type == "" {
			return nil, errors.Errorf("type required form> %q", in)
		}
		if !addGithubToken(&out) {
			continue
		}
		addAwsCredentials(&out)
		outs = append(outs, &out)
	}
	return outs, nil
}

func isRefOnlyFormat(in []string) bool {
	for _, v := range in {
		if strings.Contains(v, "=") {
			return false
		}
	}
	return true
}

func addGithubToken(ci *controllerapi.CacheOptionsEntry) bool {
	if ci.Type != "gha" {
		return true
	}
	if _, ok := ci.Attrs["token"]; !ok {
		if v, ok := os.LookupEnv("ACTIONS_RUNTIME_TOKEN"); ok {
			ci.Attrs["token"] = v
		}
	}
	if _, ok := ci.Attrs["url"]; !ok {
		if v, ok := os.LookupEnv("ACTIONS_CACHE_URL"); ok {
			ci.Attrs["url"] = v
		}
	}
	return ci.Attrs["token"] != "" && ci.Attrs["url"] != ""
}

func addAwsCredentials(ci *controllerapi.CacheOptionsEntry) {
	if ci.Type != "s3" {
		return
	}
	_, okAccessKeyID := ci.Attrs["access_key_id"]
	_, okSecretAccessKey := ci.Attrs["secret_access_key"]
	// If the user provides access_key_id, secret_access_key, do not override the session token.
	if okAccessKeyID && okSecretAccessKey {
		return
	}
	ctx := context.TODO()
	awsConfig, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return
	}
	credentials, err := awsConfig.Credentials.Retrieve(ctx)
	if err != nil {
		return
	}
	if !okAccessKeyID && credentials.AccessKeyID != "" {
		ci.Attrs["access_key_id"] = credentials.AccessKeyID
	}
	if !okSecretAccessKey && credentials.SecretAccessKey != "" {
		ci.Attrs["secret_access_key"] = credentials.SecretAccessKey
	}
	if _, ok := ci.Attrs["session_token"]; !ok && credentials.SessionToken != "" {
		ci.Attrs["session_token"] = credentials.SessionToken
	}
}
