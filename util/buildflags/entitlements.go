package buildflags

import (
	"strings"

	"github.com/moby/buildkit/util/entitlements"
	"github.com/pkg/errors"
)

const EntitlementBuildxLocalDelete = "buildx.local.delete"

func ParseEntitlements(in []string) (_ []string, allowLocalOutputDelete bool, _ error) {
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v == "" {
			continue
		}
		k, _, hasValue := strings.Cut(v, "=")
		if k == EntitlementBuildxLocalDelete {
			if hasValue {
				return nil, false, errors.Errorf("%s does not accept a value", EntitlementBuildxLocalDelete)
			}
			allowLocalOutputDelete = true
			continue
		}
		if _, _, err := entitlements.Parse(v); err != nil {
			return nil, false, err
		}
		out = append(out, v)
	}
	return out, allowLocalOutputDelete, nil
}
