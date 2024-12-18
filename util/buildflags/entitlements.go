package buildflags

import "github.com/moby/buildkit/util/entitlements"

func ParseEntitlements(in []string) ([]entitlements.Entitlement, error) {
	out := make([]entitlements.Entitlement, 0, len(in))
	for _, v := range in {
		if v == "" {
			continue
		}

		e, err := entitlements.Parse(v)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}
