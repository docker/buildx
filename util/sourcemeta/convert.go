package sourcemeta

import (
	"github.com/moby/buildkit/client/llb/sourceresolver"
	gwpb "github.com/moby/buildkit/frontend/gateway/pb"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func ToResolverOpt(req *gwpb.ResolveSourceMetaRequest, defaultPlatform *ocispecs.Platform) sourceresolver.Opt {
	platform := defaultPlatform
	if req != nil && req.Platform != nil {
		platform = &ocispecs.Platform{
			Architecture: req.Platform.Architecture,
			OS:           req.Platform.OS,
			Variant:      req.Platform.Variant,
		}
	}
	opt := sourceresolver.Opt{}
	if req != nil {
		opt.LogName = req.LogName
		opt.SourcePolicies = req.SourcePolicies
	}
	if req != nil && req.Image != nil {
		opt.ImageOpt = &sourceresolver.ResolveImageOpt{
			NoConfig:            req.Image.NoConfig,
			AttestationChain:    req.Image.AttestationChain,
			ResolveAttestations: append([]string(nil), req.Image.ResolveAttestations...),
			ResolveMode:         req.ResolveMode,
			Platform:            platform,
		}
	}
	if req != nil && req.Git != nil {
		opt.GitOpt = &sourceresolver.ResolveGitOpt{ReturnObject: req.Git.ReturnObject}
	}
	if req != nil && req.HTTP != nil && req.HTTP.ChecksumRequest != nil {
		opt.HTTPOpt = &sourceresolver.ResolveHTTPOpt{
			ChecksumReq: &sourceresolver.ResolveHTTPChecksumRequest{
				Algo:   toResolverChecksumAlgo(req.HTTP.ChecksumRequest.Algo),
				Suffix: append([]byte(nil), req.HTTP.ChecksumRequest.Suffix...),
			},
		}
	}
	return opt
}

func ToGatewayMetaResponse(resp *sourceresolver.MetaResponse) *gwpb.ResolveSourceMetaResponse {
	out := &gwpb.ResolveSourceMetaResponse{Source: resp.Op}
	if resp.Image != nil {
		out.Image = &gwpb.ResolveSourceImageResponse{
			Digest:           resp.Image.Digest.String(),
			Config:           resp.Image.Config,
			AttestationChain: toGatewayAttestationChain(resp.Image.AttestationChain),
		}
	}
	if resp.Git != nil {
		out.Git = &gwpb.ResolveSourceGitResponse{
			Checksum:       resp.Git.Checksum,
			Ref:            resp.Git.Ref,
			CommitChecksum: resp.Git.CommitChecksum,
			CommitObject:   resp.Git.CommitObject,
			TagObject:      resp.Git.TagObject,
		}
	}
	if resp.HTTP != nil {
		var lastModified *timestamppb.Timestamp
		if resp.HTTP.LastModified != nil {
			lastModified = timestamppb.New(*resp.HTTP.LastModified)
		}
		out.HTTP = &gwpb.ResolveSourceHTTPResponse{
			Checksum:     resp.HTTP.Digest.String(),
			Filename:     resp.HTTP.Filename,
			LastModified: lastModified,
		}
		if resp.HTTP.ChecksumResponse != nil {
			out.HTTP.ChecksumResponse = &gwpb.ChecksumResponse{
				Digest: resp.HTTP.ChecksumResponse.Digest,
				Suffix: append([]byte(nil), resp.HTTP.ChecksumResponse.Suffix...),
			}
		}
	}
	return out
}

func toResolverChecksumAlgo(in gwpb.ChecksumRequest_ChecksumAlgo) sourceresolver.ResolveHTTPChecksumAlgo {
	switch in {
	case gwpb.ChecksumRequest_CHECKSUM_ALGO_SHA384:
		return sourceresolver.ResolveHTTPChecksumAlgoSHA384
	case gwpb.ChecksumRequest_CHECKSUM_ALGO_SHA512:
		return sourceresolver.ResolveHTTPChecksumAlgoSHA512
	default:
		return sourceresolver.ResolveHTTPChecksumAlgoSHA256
	}
}

func toGatewayDescriptor(desc ocispecs.Descriptor) *gwpb.Descriptor {
	return &gwpb.Descriptor{
		MediaType:   desc.MediaType,
		Digest:      desc.Digest.String(),
		Size:        desc.Size,
		Annotations: desc.Annotations,
	}
}

func toGatewayAttestationChain(chain *sourceresolver.AttestationChain) *gwpb.AttestationChain {
	if chain == nil {
		return nil
	}
	signatures := make([]string, 0, len(chain.SignatureManifests))
	for _, dgst := range chain.SignatureManifests {
		signatures = append(signatures, dgst.String())
	}
	blobs := make(map[string]*gwpb.Blob, len(chain.Blobs))
	for dgst, blob := range chain.Blobs {
		blobs[dgst.String()] = &gwpb.Blob{
			Descriptor_: toGatewayDescriptor(blob.Descriptor),
			Data:        blob.Data,
		}
	}
	return &gwpb.AttestationChain{
		Root:                chain.Root.String(),
		ImageManifest:       chain.ImageManifest.String(),
		AttestationManifest: chain.AttestationManifest.String(),
		SignatureManifests:  signatures,
		Blobs:               blobs,
	}
}
