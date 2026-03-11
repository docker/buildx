package imagetools

import (
	"strings"

	"github.com/distribution/reference"
	"github.com/docker/buildx/util/ocilayout"
	digest "github.com/opencontainers/go-digest"
	"github.com/pkg/errors"
)

type LocationKind int

const (
	LocationKindRegistry LocationKind = iota
	LocationKindOCILayout
)

type Location struct {
	kind LocationKind

	original string

	named reference.Named
	oci   ocilayout.Ref
}

func ParseLocation(s string) (*Location, error) {
	if ref, ok, err := ocilayout.Parse(s); ok {
		if err != nil {
			return nil, err
		}
		return &Location{
			kind:     LocationKindOCILayout,
			original: s,
			oci:      ref,
		}, nil
	}

	ref, err := reference.ParseNormalizedNamed(s)
	if err != nil {
		return nil, err
	}
	ref = reference.TagNameOnly(ref)
	return &Location{
		kind:     LocationKindRegistry,
		original: s,
		named:    ref,
	}, nil
}

func (l *Location) String() string {
	if l == nil {
		return ""
	}
	switch l.kind {
	case LocationKindOCILayout:
		return l.oci.String()
	default:
		return l.named.String()
	}
}

func (l *Location) Kind() LocationKind {
	return l.kind
}

func (l *Location) IsRegistry() bool {
	return l != nil && l.kind == LocationKindRegistry
}

func (l *Location) IsOCILayout() bool {
	return l != nil && l.kind == LocationKindOCILayout
}

func (l *Location) Name() string {
	if l == nil {
		return ""
	}
	if l.IsRegistry() {
		return l.named.Name()
	}
	return l.oci.Path
}

func (l *Location) Named() reference.Named {
	if l == nil {
		return nil
	}
	return l.named
}

func (l *Location) OCILayout() ocilayout.Ref {
	return l.oci
}

func (l *Location) Tag() string {
	if l == nil {
		return ""
	}
	if l.IsRegistry() {
		if tagged, ok := l.named.(reference.Tagged); ok {
			return tagged.Tag()
		}
		return ""
	}
	return l.oci.Tag
}

func (l *Location) Digest() digest.Digest {
	if l == nil {
		return ""
	}
	if l.IsRegistry() {
		if digested, ok := l.named.(reference.Digested); ok {
			return digested.Digest()
		}
		return ""
	}
	return l.oci.Digest
}

func (l *Location) WithDigest(dgst digest.Digest) (*Location, error) {
	if l.IsRegistry() {
		d, err := reference.WithDigest(l.named, dgst)
		if err != nil {
			return nil, err
		}
		return &Location{kind: LocationKindRegistry, original: d.String(), named: d}, nil
	}
	ref := l.oci
	ref.Tag = ""
	ref.Digest = dgst
	return &Location{kind: LocationKindOCILayout, original: ref.String(), oci: ref}, nil
}

func (l *Location) WithTag(tag string) (*Location, error) {
	if l.IsRegistry() {
		n, err := reference.ParseNormalizedNamed(l.named.Name())
		if err != nil {
			return nil, err
		}
		t, err := reference.WithTag(n, tag)
		if err != nil {
			return nil, err
		}
		return &Location{kind: LocationKindRegistry, original: t.String(), named: t}, nil
	}
	ref := l.oci
	ref.Tag = tag
	ref.Digest = ""
	return &Location{kind: LocationKindOCILayout, original: ref.String(), oci: ref}, nil
}

func (l *Location) TagNameOnly() (*Location, error) {
	if l.IsRegistry() {
		n, err := reference.ParseNormalizedNamed(l.named.Name())
		if err != nil {
			return nil, err
		}
		return &Location{
			kind:     LocationKindRegistry,
			original: reference.TagNameOnly(n).String(),
			named:    reference.TagNameOnly(n),
		}, nil
	}
	ref := l.oci
	ref.Digest = ""
	if ref.Tag == "" {
		ref.Tag = "latest"
	}
	return &Location{kind: LocationKindOCILayout, original: ref.String(), oci: ref}, nil
}

func (l *Location) ValidateTargetDigest(desc digest.Digest) error {
	if l == nil || l.Digest() == "" {
		return nil
	}
	if l.Digest() != desc {
		return errors.Errorf("target %s requested digest %s but produced %s", l.String(), l.Digest(), desc)
	}
	return nil
}

func IsOCILayout(s string) bool {
	return strings.HasPrefix(s, "oci-layout://")
}
