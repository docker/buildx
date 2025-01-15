package imagetools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/containerd/containerd/v2/core/remotes"
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

var manifests = make(map[digest.Digest]manifest)
var indexes = make(map[digest.Digest]index)

func (f mockFetcher) Fetch(ctx context.Context, desc ocispec.Descriptor) (io.ReadCloser, error) {
	switch desc.MediaType {
	case ocispec.MediaTypeImageIndex:
		reader := io.NopCloser(strings.NewReader(indexes[desc.Digest].desc.Annotations["test_content"]))
		return reader, nil
	case ocispec.MediaTypeImageManifest:
		reader := io.NopCloser(strings.NewReader(manifests[desc.Digest].desc.Annotations["test_content"]))
		return reader, nil
	default:
		reader := io.NopCloser(strings.NewReader(desc.Annotations["test_content"]))
		return reader, nil
	}
}

func (r mockResolver) Resolve(ctx context.Context, ref string) (name string, desc ocispec.Descriptor, err error) {
	d := digest.Digest(strings.ReplaceAll(ref, "docker.io/library/test@", ""))
	return string(d), indexes[d].desc, nil
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
	return getImageFromManifests(getBaseManifests())
}

func getImageWithAttestation(t attestationType) *result {
	manifestList := getBaseManifests()

	objManifest := ocispec.Manifest{
		MediaType: v1.MediaTypeImageManifest,
		Layers:    getAttestationLayers(t),
		Annotations: map[string]string{
			"platform": "linux/amd64",
		},
	}
	jsonContent, _ := json.Marshal(objManifest)
	jsonString := string(jsonContent)
	d := digest.FromString(jsonString)

	manifestList[d] = manifest{
		desc: ocispec.Descriptor{
			MediaType: v1.MediaTypeImageManifest,
			Digest:    d,
			Size:      int64(len(jsonString)),
			Annotations: map[string]string{
				"vnd.docker.reference.digest": string(getManifestDigestForArch(manifestList, "linux", "amd64")),
				"vnd.docker.reference.type":   "attestation-manifest",
				"test_content":                jsonString,
			},
			Platform: &v1.Platform{
				Architecture: "unknown",
				OS:           "unknown",
			},
		},
		manifest: objManifest,
	}

	objManifest = ocispec.Manifest{
		MediaType: v1.MediaTypeImageManifest,
		Layers:    getAttestationLayers(t),
		Annotations: map[string]string{
			"platform": "linux/arm64",
		},
	}
	jsonContent, _ = json.Marshal(objManifest)
	jsonString = string(jsonContent)
	d = digest.FromString(jsonString)
	manifestList[d] = manifest{
		desc: ocispec.Descriptor{
			MediaType: v1.MediaTypeImageManifest,
			Digest:    d,
			Size:      int64(len(jsonString)),
			Annotations: map[string]string{
				"vnd.docker.reference.digest": string(getManifestDigestForArch(manifestList, "linux", "arm64")),
				"vnd.docker.reference.type":   "attestation-manifest",
				"test_content":                jsonString,
			},
			Platform: &v1.Platform{
				Architecture: "unknown",
				OS:           "unknown",
			},
		},
	}

	return getImageFromManifests(manifestList)
}

func getImageFromManifests(manifests map[digest.Digest]manifest) *result {
	r := &result{
		indexes:   make(map[digest.Digest]index),
		manifests: manifests,
		images:    make(map[string]digest.Digest),
		refs:      make(map[digest.Digest][]digest.Digest),
		assets:    make(map[string]asset),
	}

	r.images["linux/amd64"] = getManifestDigestForArch(manifests, "linux", "amd64")
	r.images["linux/arm64"] = getManifestDigestForArch(manifests, "linux", "arm64")

	manifestsDesc := []v1.Descriptor{}
	for _, val := range manifests {
		manifestsDesc = append(manifestsDesc, val.desc)
	}

	objIndex := v1.Index{
		MediaType: v1.MediaTypeImageIndex,
		Manifests: manifestsDesc,
	}
	jsonContent, _ := json.Marshal(objIndex)
	jsonString := string(jsonContent)
	d := digest.FromString(jsonString)

	if _, ok := indexes[d]; !ok {
		indexes[d] = index{
			desc: ocispec.Descriptor{
				MediaType: v1.MediaTypeImageIndex,
				Digest:    d,
				Size:      int64(len(jsonString)),
				Annotations: map[string]string{
					"test_content": jsonString,
				},
			},
			index: objIndex,
		}
	}

	r.indexes[d] = indexes[d]
	return r
}

func getManifestDigestForArch(manifests map[digest.Digest]manifest, os string, arch string) digest.Digest {
	for d, m := range manifests {
		if m.desc.Platform.OS == os && m.desc.Platform.Architecture == arch {
			return d
		}
	}

	return digest.Digest("")
}

func getBaseManifests() map[digest.Digest]manifest {
	if len(manifests) == 0 {
		config := getConfig()
		content := "amd64-content"
		objManifest := ocispec.Manifest{
			MediaType: v1.MediaTypeImageManifest,
			Config:    config,
			Layers: []v1.Descriptor{
				{
					MediaType: v1.MediaTypeImageLayerGzip,
					Digest:    digest.FromString(content),
					Size:      int64(len(content)),
				},
			},
		}
		jsonContent, _ := json.Marshal(objManifest)
		jsonString := string(jsonContent)
		d := digest.FromString(jsonString)

		manifests[d] = manifest{
			desc: ocispec.Descriptor{
				MediaType: v1.MediaTypeImageManifest,
				Digest:    d,
				Size:      int64(len(jsonString)),
				Platform: &v1.Platform{
					Architecture: "amd64",
					OS:           "linux",
				},
				Annotations: map[string]string{
					"test_content": jsonString,
				},
			},
			manifest: objManifest,
		}

		content = "arm64-content"
		objManifest = ocispec.Manifest{
			MediaType: v1.MediaTypeImageManifest,
			Config:    config,
			Layers: []v1.Descriptor{
				{
					MediaType: v1.MediaTypeImageLayerGzip,
					Digest:    digest.FromString(content),
					Size:      int64(len(content)),
				},
			},
		}
		jsonContent, _ = json.Marshal(objManifest)
		jsonString = string(jsonContent)
		d = digest.FromString(jsonString)

		manifests[d] = manifest{
			desc: ocispec.Descriptor{
				MediaType: v1.MediaTypeImageManifest,
				Digest:    d,
				Size:      int64(len(jsonString)),
				Platform: &v1.Platform{
					Architecture: "arm64",
					OS:           "linux",
				},
				Annotations: map[string]string{
					"test_content": jsonString,
				},
			},
			manifest: objManifest,
		}
	}

	return manifests
}

func getConfig() v1.Descriptor {
	config := v1.ImageConfig{
		Env: []string{
			"config",
		},
	}
	jsonContent, _ := json.Marshal(config)
	jsonString := string(jsonContent)
	d := digest.FromString(jsonString)

	return v1.Descriptor{
		MediaType: ocispec.MediaTypeImageConfig,
		Digest:    d,
		Size:      int64(len(jsonString)),
		Annotations: map[string]string{
			"test_content": jsonString,
		},
	}
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
