package imagetools

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/containerd/containerd/v2/core/images"
	"github.com/containerd/containerd/v2/core/remotes"
	"github.com/docker/buildx/util/resolver"
	"github.com/docker/cli/cli/config/configfile"
	"github.com/moby/buildkit/client/ociindex"
	"github.com/moby/buildkit/session/auth/authprovider"
	"github.com/opencontainers/go-digest"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
)

func TestRegistryResolverTLS(t *testing.T) {
	t.Parallel()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	cert := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	require.NoError(t, err)
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	keypair, err := tls.X509KeyPair(certPEM, keyPEM)
	require.NoError(t, err)
	pool := x509.NewCertPool()
	require.True(t, pool.AppendCertsFromPEM(certPEM))

	manifest := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.index.v1+json","manifests":[]}`)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/test/manifests/latest" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", ocispecs.MediaTypeImageIndex)
		w.Write(manifest)
	}))
	srv.TLS = &tls.Config{
		Certificates: []tls.Certificate{keypair},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    pool,
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	host := strings.TrimPrefix(srv.URL, "https://")
	plainHTTP := false
	r := New(Opt{Auth: authprovider.LoadAuthConfig(configfile.New("")), RegistryConfig: map[string]resolver.RegistryConfig{
		host: {
			PlainHTTP: &plainHTTP,
			RootCAs:   [][]byte{certPEM},
			KeyPairs: []resolver.TLSKeyPair{
				{
					Key:         keyPEM,
					Certificate: certPEM,
				},
			},
		},
	}})
	_, desc, err := r.Resolve(t.Context(), host+"/test:latest")
	require.NoError(t, err)
	require.Equal(t, digest.FromBytes(manifest), desc.Digest)
}

func TestFetchReferrersOCILayoutArtifactTypeFilter(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	idx := ociindex.NewStoreIndex(dir)
	subject := digest.FromString("subject")

	attestation := ocispecs.Descriptor{
		MediaType:    ocispecs.MediaTypeImageManifest,
		ArtifactType: artifactTypeAttestationManifest,
		Digest:       digest.FromString("attestation"),
		Size:         123,
		Annotations: map[string]string{
			images.AnnotationManifestSubject: subject.String(),
		},
	}
	require.NoError(t, idx.Put(attestation))

	signature := ocispecs.Descriptor{
		MediaType:    ocispecs.MediaTypeImageManifest,
		ArtifactType: "application/vnd.dev.sigstore.bundle.v0.3+json",
		Digest:       digest.FromString("signature"),
		Size:         456,
		Annotations: map[string]string{
			images.AnnotationManifestSubject: subject.String(),
		},
	}
	require.NoError(t, idx.Put(signature))

	loc, err := ParseLocation("oci-layout://" + dir + ":latest")
	require.NoError(t, err)

	r := New(Opt{})
	refs, err := r.FetchReferrers(context.Background(), loc, subject, remotes.WithReferrerArtifactTypes(artifactTypeAttestationManifest))
	require.NoError(t, err)
	require.Len(t, refs, 1)
	require.Equal(t, attestation.Digest, refs[0].Digest)
	require.Equal(t, attestation.ArtifactType, refs[0].ArtifactType)
}

func TestFetchReferrersOCILayoutEmpty(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	idx := ociindex.NewStoreIndex(dir)
	reachable := ocispecs.Descriptor{
		MediaType: ocispecs.MediaTypeImageManifest,
		Digest:    digest.FromString("reachable manifest"),
		Size:      123,
		Annotations: map[string]string{
			images.AnnotationManifestSubject: digest.FromString("different subject").String(),
		},
	}
	require.NoError(t, idx.Put(reachable, ociindex.Tag("latest")))

	loc, err := ParseLocation("oci-layout://" + dir + ":latest")
	require.NoError(t, err)

	r := New(Opt{})
	refs, err := r.FetchReferrers(context.Background(), loc, digest.FromString("unsigned subject"))
	require.NoError(t, err)
	require.Empty(t, refs)
}
