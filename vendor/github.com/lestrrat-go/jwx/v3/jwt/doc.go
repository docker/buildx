// Package jwt implements JSON Web Tokens as described in RFC 7519.
//
// # Parsing and Verification
//
// Parse verifies signed tokens by default. Pass WithKey, WithKeySet,
// WithKeyProvider, or WithVerifyAuto when the token is signed. A bare
// Parse call returns an error instead of silently accepting an unverified
// token.
//
// To intentionally skip verification, pass WithVerify(false). Use
// ParseInsecure only when the input is already trusted: it disables both
// signature verification and validation, and it rejects key-bearing options
// so that stray verification settings cannot silently be ignored.
//
// # Validation
//
// Parse validates registered time claims by default after decoding. In
// particular, exp, nbf, and iat are checked automatically. Validate can also
// be called directly on an existing Token. Pass WithValidate(false) to skip
// automatic validation for a particular parse.
//
// # Errors
//
// Error checks in v3 use opaque values exposed by helper functions. Use
// errors.Is with the exported helpers to check for a class of failure:
//
//	err := jwt.Validate(token)
//	if errors.Is(err, jwt.TokenExpiredError()) { /* ... */ }
//	if errors.Is(err, jwt.InvalidIssuerError()) { /* ... */ }
//
// # Time Claims
//
// Numeric date claims are decoded into time.Time values. Validation uses
// exact timestamps by default; configure WithAcceptableSkew for clock skew
// and WithTruncation when you need truncated comparisons.
//
// # OpenID Connect and Nested Tokens
//
// Use package github.com/lestrrat-go/jwx/v3/jwt/openid when you want a Token
// implementation with typed OpenID Connect claim accessors.
//
// Parse handles compact JWS JWTs and raw JSON tokens. It does not decrypt
// JWE envelopes for you. To produce nested JWTs, use
// NewSerializer().Sign(...).Encrypt(...).Serialize(...), and decrypt any
// outer JWE before calling Parse.
package jwt
