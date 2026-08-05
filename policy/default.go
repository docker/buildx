package policy

import (
	"context"
	_ "embed"
	"os"
	"strconv"
	"sync"

	"github.com/docker/buildx/util/confutil"
	"github.com/docker/buildx/util/sourcemeta"
	digest "github.com/opencontainers/go-digest"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sirupsen/logrus"
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

// DefaultPolicy returns a Policy instance backed by the embedded default
// policy module. Any files in opt are replaced: the default policy is always
// evaluated standalone.
func DefaultPolicy(opt Opt) *Policy {
	opt.Files = []File{{
		Filename: DefaultPolicyFilename,
		Data:     DefaultPolicyData(),
	}}
	return NewPolicy(opt)
}

// DefaultPolicyEnabled reports whether the builtin default policies are
// enabled via the BUILDX_DEFAULT_POLICY environment variable. It is opt-in
// for now; a future release may flip the default to on. The gate covers both
// the default source policy applied to builds and the builder-image policy
// applied when creating container builders.
var DefaultPolicyEnabled = sync.OnceValue(func() bool {
	if v, ok := os.LookupEnv("BUILDX_DEFAULT_POLICY"); ok {
		if vv, err := strconv.ParseBool(v); err == nil {
			return vv
		}
	}
	return false
})

// DefaultImageVerifier returns an image verifier backed by the builtin default
// policy, or nil when the default policy is disabled.
func DefaultImageVerifier(cfg *confutil.Config) func(context.Context, string, *ocispecs.Platform, *sourcemeta.Resolver) (digest.Digest, error) {
	if !DefaultPolicyEnabled() {
		return nil
	}
	pol := DefaultPolicy(Opt{
		Log: func(_ logrus.Level, msg string) {
			logrus.Debug(msg)
		},
		VerifierProvider: SignatureVerifier(cfg),
	})
	return func(ctx context.Context, ref string, platform *ocispecs.Platform, resolver *sourcemeta.Resolver) (digest.Digest, error) {
		return pol.CheckSource(ctx, ref, platform, resolver)
	}
}
