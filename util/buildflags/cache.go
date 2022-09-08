package buildflags

import (
	"context"
	"encoding/csv"
	"os"
	"strings"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/moby/buildkit/client"
	"github.com/pkg/errors"
)

func ParseCacheEntry(in []string) ([]client.CacheOptionsEntry, error) {
	imports := make([]client.CacheOptionsEntry, 0, len(in))
	for _, in := range in {
		csvReader := csv.NewReader(strings.NewReader(in))
		fields, err := csvReader.Read()
		if err != nil {
			return nil, err
		}
		if isRefOnlyFormat(fields) {
			for _, field := range fields {
				imports = append(imports, client.CacheOptionsEntry{
					Type:  "registry",
					Attrs: map[string]string{"ref": field},
				})
			}
			continue
		}
		im := client.CacheOptionsEntry{
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
				im.Type = value
			default:
				im.Attrs[key] = value
			}
		}
		if im.Type == "" {
			return nil, errors.Errorf("type required form> %q", in)
		}
		if !addGithubToken(&im) {
			continue
		}
		addAwsCredentials(&im)
		imports = append(imports, im)
	}
	return imports, nil
}

func isRefOnlyFormat(in []string) bool {
	for _, v := range in {
		if strings.Contains(v, "=") {
			return false
		}
	}
	return true
}

func addGithubToken(ci *client.CacheOptionsEntry) bool {
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

func addAwsCredentials(ci *client.CacheOptionsEntry) {
	if ci.Type != "s3" {
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
	if _, ok := ci.Attrs["access_key_id"]; !ok && credentials.AccessKeyID != "" {
		ci.Attrs["access_key_id"] = credentials.AccessKeyID
	}
	if _, ok := ci.Attrs["secret_access_key"]; !ok && credentials.SecretAccessKey != "" {
		ci.Attrs["secret_access_key"] = credentials.SecretAccessKey
	}
	if _, ok := ci.Attrs["session_token"]; !ok && credentials.SessionToken != "" {
		ci.Attrs["session_token"] = credentials.SessionToken
	}
}
