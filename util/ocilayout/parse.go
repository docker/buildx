package ocilayout

import (
	"strings"

	"github.com/distribution/reference"
	digest "github.com/opencontainers/go-digest"
)

type Ref struct {
	Path   string
	Tag    string
	Digest digest.Digest
}

const prefix = "oci-layout://"

func Parse(s string) (Ref, bool, error) {
	if !strings.HasPrefix(s, prefix) {
		return Ref{}, false, nil
	}

	localPath := strings.TrimPrefix(s, prefix)
	var out Ref

	if i := strings.LastIndex(localPath, "@"); i >= 0 {
		after := localPath[i+1:]
		if reference.DigestRegexp.MatchString(after) {
			dgst, err := digest.Parse(after)
			if err != nil {
				return Ref{}, true, err
			}
			localPath, out.Digest = localPath[:i], dgst
		}
	}

	if i := strings.LastIndex(localPath, ":"); i >= 0 {
		after := localPath[i+1:]
		if reference.TagRegexp.MatchString(after) {
			localPath, out.Tag = localPath[:i], after
		}
	}

	out.Path = localPath
	if out.Tag == "" && out.Digest == "" {
		out.Tag = "latest"
	}
	return out, true, nil
}

func (r Ref) String() string {
	s := prefix + r.Path
	if r.Tag != "" {
		return s + ":" + r.Tag
	}
	if r.Digest != "" {
		return s + "@" + r.Digest.String()
	}
	return s
}
