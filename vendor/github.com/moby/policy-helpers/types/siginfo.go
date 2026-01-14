package types

import (
	"time"

	"github.com/sigstore/sigstore-go/pkg/fulcio/certificate"
)

type TimestampVerificationResult struct {
	Type      string    `json:"type"`
	URI       string    `json:"uri"`
	Timestamp time.Time `json:"timestamp"`
}

type TrustRootStatus struct {
	Error       string     `json:"error,omitempty"`
	LastUpdated *time.Time `json:"lastUpdated,omitempty"`
}

type SignatureInfo struct {
	Signer          *certificate.Summary          `json:"signer,omitempty"`
	Timestamps      []TimestampVerificationResult `json:"timestamps,omitempty"`
	DockerReference string                        `json:"dockerReference,omitempty"`
	TrustRootStatus TrustRootStatus               `json:"trustRootStatus,omitzero"`
	IsDHI           bool                          `json:"isDHI,omitempty"`
}
