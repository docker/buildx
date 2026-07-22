package bkimage

const (
	DefaultImage         = "moby/buildkit:buildx-stable-1"
	QemuImage            = "tonistiigi/binfmt:latest" // TODO: make this verified
	DefaultRootlessImage = DefaultImage + "-rootless"

	// TrustedRepo is the fully-qualified repository whose tags are verified
	// against the builtin default policy before a builder is created from
	// them. Images from other repositories pass through unverified; the
	// allow-untrusted-image driver-opt is only needed when a TrustedRepo tag
	// that the policy covers does not verify correctly.
	TrustedRepo = "docker.io/moby/buildkit"
)
