package policy

import (
	"path"
	"strconv"
	"strings"

	"github.com/distribution/reference"
	"github.com/docker/buildx/util/urlutil"
	slsa1 "github.com/in-toto/in-toto-golang/in_toto/slsa_provenance/v1"
	"github.com/moby/buildkit/solver/pb"
	"github.com/moby/buildkit/util/gitutil"
	"github.com/moby/buildkit/util/purl"
	"github.com/opencontainers/go-digest"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
)

func isMaterialKey(key string) (idx int, rest string, ok bool) {
	const prefix = "image.provenance.materials["
	if !strings.HasPrefix(key, prefix) {
		return 0, "", false
	}
	rest = strings.TrimPrefix(key, prefix)
	end := strings.IndexByte(rest, ']')
	if end < 0 {
		return 0, "", false
	}
	n, err := strconv.Atoi(rest[:end])
	if err != nil {
		return 0, "", false
	}
	rest = strings.TrimPrefix(rest[end+1:], ".")
	return n, rest, true
}

func ParseSLSAMaterial(m slsa1.ResourceDescriptor) (*pb.SourceOp, *ocispecs.Platform, error) {
	uri := m.URI
	dgst := m.Digest
	if strings.HasPrefix(uri, "pkg:docker/") {
		return dockerMaterialSource(uri, dgst)
	}

	if gu, err := gitutil.ParseURL(uri); err == nil {
		if strings.HasSuffix(strings.ToLower(gu.Path), ".git") || gu.Scheme != gitutil.HTTPProtocol && gu.Scheme != gitutil.HTTPSProtocol {
			return gitMaterialSource(uri)
		}
	}

	if urlutil.IsHTTPURL(uri) {
		return &pb.SourceOp{Identifier: uri}, nil, nil
	}

	return nil, nil, errors.Errorf("unsupported material URI %q", uri)
}

func dockerMaterialSource(uri string, dgst map[string]string) (*pb.SourceOp, *ocispecs.Platform, error) {
	refStr, platform, err := purl.PURLToRef(uri)
	if err != nil {
		return nil, nil, err
	}

	named, err := reference.ParseNormalizedNamed(refStr)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "invalid docker reference %q from %q", refStr, uri)
	}
	if checksum := strings.TrimSpace(dgst["sha256"]); checksum != "" {
		dgstRef, err := digest.Parse(checksum)
		if err != nil {
			dgstRef, err = digest.Parse("sha256:" + checksum)
		}
		if err != nil {
			return nil, nil, errors.Wrapf(err, "invalid material digest %q for %q", checksum, uri)
		}
		if canonical, ok := named.(reference.Canonical); ok {
			if canonical.Digest() != dgstRef {
				return nil, nil, errors.Errorf("material digest mismatch for %q: ref has %s but provenance has %s", uri, canonical.Digest(), dgstRef)
			}
		} else {
			named, err = reference.WithDigest(named, dgstRef)
			if err != nil {
				return nil, nil, errors.Wrapf(err, "failed to add digest %q to %q", dgstRef, refStr)
			}
		}
	}

	return &pb.SourceOp{Identifier: "docker-image://" + named.String()}, platform, nil
}

func gitMaterialSource(uri string) (*pb.SourceOp, *ocispecs.Platform, error) {
	gu, err := gitutil.ParseURL(uri)
	if err != nil {
		return nil, nil, err
	}

	switch gu.Scheme {
	case gitutil.HTTPSProtocol, gitutil.HTTPProtocol, gitutil.SSHProtocol, gitutil.GitProtocol:
	default:
		return nil, nil, errors.Errorf("unsupported git material URI %q", uri)
	}

	id := gu.Host + path.Join("/", gu.Path)
	if gu.Opts != nil && (gu.Opts.Ref != "" || gu.Opts.Subdir != "") {
		id += "#" + gu.Opts.Ref
		if gu.Opts.Subdir != "" {
			id += ":" + gu.Opts.Subdir
		}
	}

	return &pb.SourceOp{
		Identifier: "git://" + id,
		Attrs: map[string]string{
			pb.AttrFullRemoteURL: uri,
		},
	}, nil, nil
}
