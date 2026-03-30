package sourcemeta

import (
	"testing"

	"github.com/moby/buildkit/client/llb/sourceresolver"
	gwpb "github.com/moby/buildkit/frontend/gateway/pb"
	"github.com/moby/buildkit/solver/pb"
	"github.com/stretchr/testify/require"
)

func TestToResolverOptHTTPChecksumRequest(t *testing.T) {
	opt := ToResolverOpt(&gwpb.ResolveSourceMetaRequest{
		Source: &pb.SourceOp{Identifier: "https://example.com/a"},
		HTTP: &gwpb.ResolveSourceHTTPRequest{
			ChecksumRequest: &gwpb.ChecksumRequest{
				Algo:   gwpb.ChecksumRequest_CHECKSUM_ALGO_SHA512,
				Suffix: []byte{0x01, 0x02},
			},
		},
	}, nil)
	require.NotNil(t, opt.HTTPOpt)
	require.NotNil(t, opt.HTTPOpt.ChecksumReq)
	require.Equal(t, sourceresolver.ResolveHTTPChecksumAlgoSHA512, opt.HTTPOpt.ChecksumReq.Algo)
	require.Equal(t, []byte{0x01, 0x02}, opt.HTTPOpt.ChecksumReq.Suffix)
}

func TestToGatewayMetaResponseHTTPChecksumResponse(t *testing.T) {
	out := ToGatewayMetaResponse(&sourceresolver.MetaResponse{
		Op: &pb.SourceOp{Identifier: "https://example.com/a"},
		HTTP: &sourceresolver.ResolveHTTPResponse{
			Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			ChecksumResponse: &sourceresolver.ResolveHTTPChecksumResponse{
				Digest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
				Suffix: []byte{0x03, 0x04},
			},
		},
	})
	require.NotNil(t, out.HTTP)
	require.NotNil(t, out.HTTP.ChecksumResponse)
	require.Equal(t, "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", out.HTTP.ChecksumResponse.Digest)
	require.Equal(t, []byte{0x03, 0x04}, out.HTTP.ChecksumResponse.Suffix)
}
