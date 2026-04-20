package policy

import (
	_ "embed"
)

// DefaultPolicyFilename is the synthetic filename used for the embedded
// default policy when it is loaded as a regular policy file.
const DefaultPolicyFilename = "buildx_default_policy.rego"

//go:embed default.rego
var defaultPolicyModule []byte

// DefaultPolicyData returns the embedded default policy module bytes.
func DefaultPolicyData() []byte {
	return defaultPolicyModule
}
