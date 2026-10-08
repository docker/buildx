# AGENTS.md

## For Module Consumers

If you are writing code that *uses* jwx (not developing jwx itself):

- **Examples**: See `examples/` directory for runnable usage patterns
- **Documentation**: See `docs/` directory and package READMEs
- **API Reference**: Use `go doc` or https://pkg.go.dev/github.com/lestrrat-go/jwx/v3

The rest of this document focuses on developing the jwx library itself.

---

## Go Version

This project requires **Go 1.25.0** or later. Check `go.mod` for the exact version.

## Module Path vs Physical Layout

This repository uses a **flat layout** with vanity import paths. There is no physical `v3/` directory.

| Branch | Module Path | Physical Root |
|--------|-------------|---------------|
| `develop/v3` | `github.com/lestrrat-go/jwx/v3` | `/` (repo root) |

`import "github.com/lestrrat-go/jwx/v3/jwt"` → files are at `./jwt/`, not `./v3/jwt/`.

## Code Generation

### Immutable Rule

**NEVER edit files ending in `_gen.go` directly.** These are generated files. Edit the generator sources instead.

### Generated Files Pattern

Files matching `*_gen.go` are generated. Examples:
- `jwt/options_gen.go`
- `jwt/token_gen.go`
- `jws/headers_gen.go`
- `jwk/rsa_gen.go`
- `jwa/signature_gen.go`

### Generator Locations

| Generator | Location | Input Files | Output |
|-----------|----------|-------------|--------|
| `genoptions` | `tools/cmd/genoptions/` | `{jwa,jwe,jwk,jws,jwt}/options.yaml` | `*/options_gen.go` |
| `genjwt` | `tools/cmd/genjwt/` | `tools/cmd/genjwt/objects.yml` | `jwt/*_gen.go` |
| `genjws` | `tools/cmd/genjws/` | `tools/cmd/genjws/objects.yml` | `jws/*_gen.go` |
| `genjwe` | `tools/cmd/genjwe/` | `tools/cmd/genjwe/objects.yml` | `jwe/*_gen.go` |
| `genjwk` | `tools/cmd/genjwk/` | `tools/cmd/genjwk/objects.yml` | `jwk/*_gen.go` |
| `genjwa` | `tools/cmd/genjwa/` | `tools/cmd/genjwa/objects.yml` | `jwa/*_gen.go` |
| `genreadfile` | `tools/cmd/genreadfile/` | - | ReadFile helpers |

### Regeneration Commands

```bash
# Regenerate all code (includes options via `go generate .`)
make generate

# Regenerate specific package (objects/types only, NOT options)
make generate-jwt
make generate-jws
make generate-jwe
make generate-jwk
make generate-jwa

# Regenerate options only (options.yaml → options_gen.go for all packages)
go generate .
# or directly:
./tools/cmd/genoptions.sh
```

**Important:** `make generate-<pkg>` does **not** regenerate options. If you
edit an `options.yaml` file, run `make generate` or `go generate .`.

## Functional Options Pattern

Options are defined in `{package}/options.yaml` and generated into `{package}/options_gen.go`.

Example `options.yaml` entry:

```yaml
options:
  - ident: Token
    interface: ParseOption
    argument_type: Token
    comment: |
      WithToken specifies the token instance...
```

Generates `WithToken(v Token) ParseOption` function.

## Multi-Module Structure

This repository contains multiple Go modules. The nested modules use `replace` directives for local development.

| Module | Path | Purpose |
|--------|------|---------|
| Main | `./go.mod` | Core library |
| Examples | `./examples/go.mod` | Usage examples |
| CLI | `./cmd/jwx/go.mod` | Command-line tool |
| Perf Bench | `./bench/performance/go.mod` | Performance benchmarks |
| Comparison | `./bench/comparison/go.mod` | Library comparison |
| Generators | `./tools/cmd/*/go.mod` | Code generators |

### Local Development

The `examples/go.mod` contains:
```go
replace github.com/lestrrat-go/jwx/v3 v3.0.0 => ../
```

No `go.work` file is committed. When working across modules, either:
1. Create a temporary `go.work` file (it is .gitignored)
2. Rely on the `replace` directives already in place

## Development Commands

```bash
# Run all tests
make test

# Run tests with specific build tags
make test-goccy       # Use goccy/go-json
make test-es256k      # Enable ES256K support
make test-alltags     # All optional features

# Run short/smoke tests
make smoke

# Generate coverage report
make cover
make viewcover

# Lint
make lint

# Format and tidy
make imports
make tidy
```

### Test Script Details

Tests are run via `./tools/test.sh` which iterates over:
- `.` (main module)
- `./examples`
- `./bench/performance`
- `./cmd/jwx`

## Package Directory Map

| Package | Responsibility |
|---------|----------------|
| `jwa/` | Algorithm identifiers (e.g., `RS256`, `ES384`, `A128GCM`) |
| `jwk/` | JSON Web Keys - key representation and management |
| `jws/` | JSON Web Signatures - `Sign()` and `Verify()` |
| `jwe/` | JSON Web Encryption - `Encrypt()` and `Decrypt()` |
| `jwt/` | JSON Web Tokens - claims and validation |
| `jwt/openid/` | OpenID Connect ID tokens |
| `transform/` | Token transformation utilities |

## Relevant RFCs

- RFC 7515 - JWS (JSON Web Signature)
- RFC 7516 - JWE (JSON Web Encryption)
- RFC 7517 - JWK (JSON Web Key)
- RFC 7518 - JWA (JSON Web Algorithms)
- RFC 7519 - JWT (JSON Web Token)
- OpenID Connect Core 1.0

## Error Handling

Sentinel errors are exposed via functions. Use `errors.Is()`:

```go
if errors.Is(err, jwt.TokenExpiredError()) { ... }
```

| Package | Function | Meaning |
|---------|----------|---------|
| `jwt` | `TokenExpiredError()` | `exp` claim not satisfied |
| `jwt` | `TokenNotYetValidError()` | `nbf` claim not satisfied |
| `jwt` | `InvalidIssuerError()` | `iss` claim not satisfied |
| `jwt` | `InvalidAudienceError()` | `aud` claim not satisfied |
| `jwt` | `ValidateError()` | Generic validation failure |
| `jwt` | `ParseError()` | Parse failed |
| `jws` | `VerificationError()` | Signature verification failed |
| `jwe` | `DecryptError()` | Decryption failed |

## Testing

Use `github.com/stretchr/testify/require` for assertions (not `assert`).

## Build Tags

| Tag | Effect |
|-----|--------|
| `jwx_goccy` | Use `goccy/go-json` instead of `encoding/json` |
| `jwx_es256k` | Enable secp256k1/ES256K algorithm support |
| `jwx_secp256k1_pem` | Enable PEM encoding for secp256k1 keys |
| `jwx_asmbase64` | Use assembly-optimized base64 |

## Quick Reference: Common Modifications

| Task | Edit This | Then Run |
|------|-----------|----------|
| Add/edit any option | `{pkg}/options.yaml` | `make generate` or `go generate .` |
| Add new JWS header field | `tools/cmd/genjws/objects.yml` | `make generate-jws` |
| Add new JWK key field | `tools/cmd/genjwk/objects.yml` | `make generate-jwk` |
| Add new algorithm | `tools/cmd/genjwa/objects.yml` | `make generate-jwa` |
| Modify token fields | `tools/cmd/genjwt/objects.yml` | `make generate-jwt` |

## File Naming Conventions

| Pattern | Meaning |
|---------|---------|
| `*_gen.go` | Generated code - DO NOT EDIT |
| `*_test.go` | Test files |
| `*_gen_test.go` | Generated tests - DO NOT EDIT |
| `options.yaml` | Option definitions (input to genoptions) |
| `objects.yml` | Object definitions (input to package-specific generators) |

## Examples Directory

Naming convention: `{package}_xxx_example_test.go`
- `jwt_parse_example_test.go`
- `jws_sign_example_test.go`
- `jwx_example_test.go` (cross-package)
- `jwx_readme_example_test.go` (cross-package, used in README)
- `jwx_register_ec_and_key_example_test.go` (cross-package, key registration)

Examples are included in `docs/` via autodoc markers:
```markdown
<!-- INCLUDE(examples/jwt_parse_example_test.go) -->
<!-- END INCLUDE -->
```

## Pre-Read Rules

Read linked doc BEFORE working in that area. No exceptions.

| Trigger | Doc |
|---------|-----|
| Looking up package APIs, types, functions | `.claude/docs/packages.md` |
| Running or writing tests, fuzz tests | `.claude/docs/testing.md` |
| Understanding package relationships, imports | `.claude/docs/dependencies.md` |
| Working with errors, error handling patterns | `.claude/docs/error-formatting.md` |
| Code generation, options pattern, extension points, JSON/base64 backends | `.claude/docs/internals.md` |

## Cache Maintenance

These docs cache repository state. Still read source before modifying code.

1. When your changes affect a doc below, update it in the same commit.
2. If you notice any doc is wrong or stale — even on an unrelated task — fix it immediately.

| Doc | Update trigger |
|-----|----------------|
| `.claude/docs/packages.md` | New/renamed/removed exported functions, types, or packages |
| `.claude/docs/testing.md` | Changes to test infrastructure, build tags, test helpers, fuzz targets |
| `.claude/docs/dependencies.md` | New internal imports between packages, new external dependencies |
| `.claude/docs/error-formatting.md` | New sentinel errors, changes to error wrapping patterns |
| `.claude/docs/internals.md` | Changes to generators, options YAML schema, registration points, multi-module layout |
