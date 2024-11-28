package buildflags

import (
	"context"
	"encoding/json"
	"maps"
	"os"
	"strings"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	controllerapi "github.com/docker/buildx/controller/pb"
	"github.com/pkg/errors"
	"github.com/tonistiigi/go-csvvalue"
	"github.com/zclconf/go-cty/cty"
	jsoncty "github.com/zclconf/go-cty/cty/json"
)

type CacheOptionsEntry struct {
	Type         string            `json:"type"`
	Attrs        map[string]string `json:"attrs,omitempty"`
	DerivedAttrs map[string]string `json:"-"`
}

func (e *CacheOptionsEntry) Equal(other *CacheOptionsEntry) bool {
	if e.Type != other.Type {
		return false
	}
	return maps.Equal(e.Attrs, other.Attrs)
}

func (e *CacheOptionsEntry) String() string {
	// Special registry syntax.
	if e.Type == "registry" && len(e.Attrs) == 1 {
		if ref, ok := e.Attrs["ref"]; ok {
			return ref
		}
	}

	var b csvBuilder
	if e.Type != "" {
		b.Write("type", e.Type)
	}
	if len(e.Attrs) > 0 {
		b.WriteAttributes(e.Attrs)
	}
	return b.String()
}

func (e *CacheOptionsEntry) ToPB() *controllerapi.CacheOptionsEntry {
	attrs := maps.Clone(e.Attrs)
	maps.Copy(attrs, e.DerivedAttrs)
	return &controllerapi.CacheOptionsEntry{
		Type:  e.Type,
		Attrs: attrs,
	}
}

func (e *CacheOptionsEntry) MarshalJSON() ([]byte, error) {
	m := maps.Clone(e.Attrs)
	if m == nil {
		m = map[string]string{}
	}
	m["type"] = e.Type
	return json.Marshal(m)
}

func (e *CacheOptionsEntry) UnmarshalJSON(data []byte) error {
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}

	e.Type = m["type"]
	delete(m, "type")

	e.Attrs = m
	return e.validate(data)
}

func (e *CacheOptionsEntry) IsActive() bool {
	// Always active if not gha.
	if e.Type != "gha" {
		return true
	}
	return (e.Attrs["token"] != "" || e.DerivedAttrs["token"] != "") && (e.Attrs["url"] != "" || e.DerivedAttrs["url"] != "")
}

func (e *CacheOptionsEntry) UnmarshalText(text []byte) error {
	in := string(text)
	fields, err := csvvalue.Fields(in, nil)
	if err != nil {
		return err
	}

	if len(fields) == 1 && !strings.Contains(fields[0], "=") {
		e.Type = "registry"
		e.Attrs = map[string]string{"ref": fields[0]}
		return nil
	}

	e.Type = ""
	e.Attrs = map[string]string{}
	e.DerivedAttrs = map[string]string{}

	for _, field := range fields {
		parts := strings.SplitN(field, "=", 2)
		if len(parts) != 2 {
			return errors.Errorf("invalid value %s", field)
		}
		key := strings.ToLower(parts[0])
		value := parts[1]
		switch key {
		case "type":
			e.Type = value
		default:
			e.Attrs[key] = value
		}
	}

	if e.Type == "" {
		return errors.Errorf("type required form> %q", in)
	}
	addGithubToken(e)
	addAwsCredentials(e)
	return e.validate(text)
}

func (e *CacheOptionsEntry) validate(gv interface{}) error {
	if e.Type == "" {
		var text []byte
		switch gv := gv.(type) {
		case []byte:
			text = gv
		case string:
			text = []byte(gv)
		case cty.Value:
			text, _ = jsoncty.Marshal(gv, gv.Type())
		default:
			text, _ = json.Marshal(gv)
		}
		return errors.Errorf("type required form> %q", string(text))
	}
	return nil
}

func ParseCacheEntry(in []string) ([]*controllerapi.CacheOptionsEntry, error) {
	outs := make([]*controllerapi.CacheOptionsEntry, 0, len(in))
	for _, in := range in {
		var out CacheOptionsEntry
		if err := out.UnmarshalText([]byte(in)); err != nil {
			return nil, err
		}

		if !out.IsActive() {
			// Skip inactive cache entries.
			continue
		}
		outs = append(outs, out.ToPB())
	}
	return outs, nil
}

func addGithubToken(ci *CacheOptionsEntry) {
	if ci.Type != "gha" {
		return
	}
	if _, ok := ci.Attrs["token"]; !ok {
		if v, ok := os.LookupEnv("ACTIONS_RUNTIME_TOKEN"); ok {
			ci.DerivedAttrs["token"] = v
		}
	}
	if _, ok := ci.Attrs["url"]; !ok {
		if v, ok := os.LookupEnv("ACTIONS_CACHE_URL"); ok {
			ci.DerivedAttrs["url"] = v
		}
	}
}

func addAwsCredentials(ci *CacheOptionsEntry) {
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
		ci.DerivedAttrs["access_key_id"] = credentials.AccessKeyID
	}
	if !okSecretAccessKey && credentials.SecretAccessKey != "" {
		ci.DerivedAttrs["secret_access_key"] = credentials.SecretAccessKey
	}
	if _, ok := ci.Attrs["session_token"]; !ok && credentials.SessionToken != "" {
		ci.DerivedAttrs["session_token"] = credentials.SessionToken
	}
}
