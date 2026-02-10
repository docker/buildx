package policy

import (
	"testing"

	gwpb "github.com/moby/buildkit/frontend/gateway/pb"
	"github.com/stretchr/testify/require"
)

func TestAddUnknowns(t *testing.T) {
	tests := []struct {
		name      string
		unknowns  []string
		initial   *gwpb.ResolveSourceMetaRequest
		expected  *gwpb.ResolveSourceMetaRequest
		expErrMsg string
	}{
		{
			name:     "empty-unknowns",
			unknowns: nil,
			initial:  &gwpb.ResolveSourceMetaRequest{},
			expected: &gwpb.ResolveSourceMetaRequest{},
		},
		{
			name:     "parent-key-ignored",
			unknowns: []string{"image"},
			initial:  &gwpb.ResolveSourceMetaRequest{},
			expected: &gwpb.ResolveSourceMetaRequest{},
		},
		{
			name:     "image-config-fields-enable-image-request",
			unknowns: []string{"image.labels"},
			initial:  &gwpb.ResolveSourceMetaRequest{},
			expected: &gwpb.ResolveSourceMetaRequest{
				Image: &gwpb.ResolveSourceImageRequest{},
			},
		},
		{
			name:     "image-attestation-fields-enable-attestation-chain",
			unknowns: []string{"image.signatures"},
			initial:  &gwpb.ResolveSourceMetaRequest{},
			expected: &gwpb.ResolveSourceMetaRequest{
				Image: &gwpb.ResolveSourceImageRequest{
					NoConfig:         true,
					AttestationChain: true,
				},
			},
		},
		{
			name:     "image-attestation-on-existing-image-request",
			unknowns: []string{"image.hasProvenance"},
			initial: &gwpb.ResolveSourceMetaRequest{
				Image: &gwpb.ResolveSourceImageRequest{
					NoConfig: false,
				},
			},
			expected: &gwpb.ResolveSourceMetaRequest{
				Image: &gwpb.ResolveSourceImageRequest{
					NoConfig:         false,
					AttestationChain: true,
				},
			},
		},
		{
			name:     "git-ref-field-enables-git-request",
			unknowns: []string{"git.ref"},
			initial:  &gwpb.ResolveSourceMetaRequest{},
			expected: &gwpb.ResolveSourceMetaRequest{
				Git: &gwpb.ResolveSourceGitRequest{},
			},
		},
		{
			name:     "git-commit-enables-return-object",
			unknowns: []string{"git.commit"},
			initial:  &gwpb.ResolveSourceMetaRequest{},
			expected: &gwpb.ResolveSourceMetaRequest{
				Git: &gwpb.ResolveSourceGitRequest{
					ReturnObject: true,
				},
			},
		},
		{
			name:     "http-checksum-no-op",
			unknowns: []string{"http.checksum"},
			initial:  &gwpb.ResolveSourceMetaRequest{},
			expected: &gwpb.ResolveSourceMetaRequest{},
		},
		{
			name:      "non-canonical-input-prefix-errors",
			unknowns:  []string{"input.image.labels"},
			initial:   &gwpb.ResolveSourceMetaRequest{},
			expErrMsg: "unhandled unknown property input.image.labels",
		},
		{
			name:      "unknown-field-errors",
			unknowns:  []string{"git.notAField"},
			initial:   &gwpb.ResolveSourceMetaRequest{},
			expErrMsg: "unhandled unknown property git.notAField",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := tc.initial
			if req == nil {
				req = &gwpb.ResolveSourceMetaRequest{}
			}
			err := AddUnknowns(req, tc.unknowns)
			if tc.expErrMsg != "" {
				require.Error(t, err)
				require.Equal(t, tc.expErrMsg, err.Error())
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.expected, req)
		})
	}
}
