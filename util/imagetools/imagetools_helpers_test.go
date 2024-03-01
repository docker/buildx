package imagetools

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"strings"

	"github.com/containerd/containerd/remotes"
	intoto "github.com/in-toto/in-toto-golang/in_toto"
	slsa02 "github.com/in-toto/in-toto-golang/in_toto/slsa_provenance/v0.2"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
)

type attestationType int

const (
	plainSpdx             attestationType = 0
	dsseEmbeded           attestationType = 1
	plainSpdxAndDSSEEmbed attestationType = 2
)

type mockFetcher struct {
}

type mockResolver struct {
	fetcher remotes.Fetcher
	pusher  remotes.Pusher
}

func (f mockFetcher) Fetch(ctx context.Context, desc ocispec.Descriptor) (io.ReadCloser, error) {
	reader := io.NopCloser(strings.NewReader(desc.Annotations["test_content"]))
	return reader, nil
}

func (r mockResolver) Resolve(ctx context.Context, ref string) (name string, desc ocispec.Descriptor, err error) {
	return "", ocispec.Descriptor{}, nil
}

func (r mockResolver) Fetcher(ctx context.Context, ref string) (remotes.Fetcher, error) {
	return r.fetcher, nil
}

func (r mockResolver) Pusher(ctx context.Context, ref string) (remotes.Pusher, error) {
	return r.pusher, nil
}

func getMockResolver() remotes.Resolver {
	resolver := mockResolver{
		fetcher: mockFetcher{},
	}

	return resolver
}

func getImageNoAttestation() *result {
	r := &result{
		indexes:   make(map[digest.Digest]index),
		manifests: make(map[digest.Digest]manifest),
		images:    make(map[string]digest.Digest),
		refs:      make(map[digest.Digest][]digest.Digest),
		assets:    make(map[string]asset),
	}

	r.images["linux/amd64"] = "sha256:linux/amd64"
	r.images["linux/arm64"] = "sha256:linux/arm64"

	r.manifests["sha256:linux/amd64-manifest"] = manifest{
		desc: ocispec.Descriptor{
			MediaType: v1.MediaTypeImageManifest,
			Digest:    "sha256:linux/amd64-manifest",
			Platform: &v1.Platform{
				Architecture: "amd64",
				OS:           "linux",
			},
		},
		manifest: ocispec.Manifest{
			MediaType: v1.MediaTypeImageManifest,
			Layers: []v1.Descriptor{
				{
					MediaType: v1.MediaTypeImageLayerGzip,
					Digest:    "sha256:linux/amd64-content",
					Size:      1234,
				},
			},
		},
	}
	r.manifests["sha256:linux/arm64-manifest"] = manifest{
		desc: ocispec.Descriptor{
			MediaType: v1.MediaTypeImageManifest,
			Digest:    "sha256:linux/arm64-manifest",
			Platform: &v1.Platform{
				Architecture: "arm64",
				OS:           "linux",
			},
		},
		manifest: ocispec.Manifest{
			MediaType: v1.MediaTypeImageManifest,
			Layers: []v1.Descriptor{
				{
					MediaType: v1.MediaTypeImageLayerGzip,
					Digest:    "sha256:linux/arm64-content",
					Size:      1234,
				},
			},
		},
	}

	return r
}

func getImageWithAttestation(t attestationType) *result {
	r := getImageNoAttestation()

	r.manifests["sha256:linux/amd64-attestation"] = manifest{
		desc: ocispec.Descriptor{
			MediaType: v1.MediaTypeImageManifest,
			Digest:    "sha256:linux/amd64-attestation",
			Annotations: map[string]string{
				"vnd.docker.reference.digest": "sha256:linux/amd64",
				"vnd.docker.reference.type":   "attestation-manifest",
			},
			Platform: &v1.Platform{
				Architecture: "unknown",
				OS:           "unknown",
			},
		},
		manifest: ocispec.Manifest{
			MediaType: v1.MediaTypeImageManifest,
			Layers:    getAttestationLayers(t),
		},
	}

	return r
}

func getAttestationLayers(t attestationType) []v1.Descriptor {
	layers := []v1.Descriptor{}

	if t == plainSpdx || t == plainSpdxAndDSSEEmbed {
		layers = append(layers, v1.Descriptor{
			MediaType: inTotoGenericMime,
			Digest:    digest.FromString(attestationContent),
			Size:      int64(len(attestationContent)),
			Annotations: map[string]string{
				"in-toto.io/predicate-type": intoto.PredicateSPDX,
				"test_content":              attestationContent,
			},
		})
		layers = append(layers, v1.Descriptor{
			MediaType: inTotoGenericMime,
			Digest:    digest.FromString(provenanceContent),
			Size:      int64(len(provenanceContent)),
			Annotations: map[string]string{
				"in-toto.io/predicate-type": slsa02.PredicateSLSAProvenance,
				"test_content":              provenanceContent,
			},
		})
	}

	if t == dsseEmbeded || t == plainSpdxAndDSSEEmbed {
		dsseAttestation := fmt.Sprintf("{\"payload\":\"%s\"}", base64.StdEncoding.EncodeToString([]byte(attestationContent)))
		dsseProvenance := fmt.Sprintf("{\"payload\":\"%s\"}", base64.StdEncoding.EncodeToString([]byte(provenanceContent)))
		layers = append(layers, v1.Descriptor{
			MediaType: inTotoSPDXDSSEMime,
			Digest:    digest.FromString(dsseAttestation),
			Size:      int64(len(dsseAttestation)),
			Annotations: map[string]string{
				"in-toto.io/predicate-type": intoto.PredicateSPDX,
				"test_content":              dsseAttestation,
			},
		})
		layers = append(layers, v1.Descriptor{
			MediaType: inTotoProvenanceDSSEMime,
			Digest:    digest.FromString(dsseProvenance),
			Size:      int64(len(dsseProvenance)),
			Annotations: map[string]string{
				"in-toto.io/predicate-type": slsa02.PredicateSLSAProvenance,
				"test_content":              dsseProvenance,
			},
		})
	}

	return layers
}

const attestationContent = `
{
    "_type": "https://in-toto.io/Statement/v0.1",
    "predicateType": "https://spdx.dev/Document",
    "predicate": {
		"name": "sbom",
		"spdxVersion": "SPDX-2.3",
		"SPDXID": "SPDXRef-DOCUMENT",
		"creationInfo": {
			"created": "2024-01-31T16:09:05Z",
			"creators": [
				"Tool: buildkit-v0.11.0"
			],
			"licenseListVersion": "3.22"
		},
		"dataLicense": "CC0-1.0",
		"documentNamespace": "https://example.com",
		"packages": [
			{
				"name": "sbom",
				"SPDXID": "SPDXRef-DocumentRoot-Directory-sbom",
				"copyrightText": "",
				"downloadLocation": "NOASSERTION",
				"primaryPackagePurpose": "FILE",
				"supplier": "NOASSERTION"
			}
		],
		"relationships": [
			{
				"relatedSpdxElement": "SPDXRef-DocumentRoot-Directory-sbom",
				"relationshipType": "DESCRIBES",
				"spdxElementId": "SPDXRef-DOCUMENT"
			}
		]
	}
}
`

const provenanceContent = `
{
	"_type": "https://in-toto.io/Statement/v0.1",
	"predicateType": "https://slsa.dev/provenance/v0.2",
	"predicate": {
		"buildType": "https://example.com/Makefile",
		"builder": { 
			"id": "mailto:person@example.com"
		},
		"invocation": {
			"configSource": {
				"uri": "https://example.com/example-1.2.3.tar.gz",
				"digest": {"sha256": ""},
				"entryPoint": "src:foo"
			},
			"parameters": {
				"CFLAGS": "-O3"
			},
			"materials": [
				{
					"uri": "https://example.com/example-1.2.3.tar.gz",
					"digest": {"sha256": ""}
				}
			]
		}
	}
}
`
