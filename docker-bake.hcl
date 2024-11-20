variable "GO_VERSION" {
  default = null
}
variable "DOCS_FORMATS" {
  default = "md"
}
variable "DESTDIR" {
  default = "./bin"
}
variable "TEST_COVERAGE" {
  default = null
}
variable "GOLANGCI_LINT_MULTIPLATFORM" {
  default = ""
}

# Special target: https://github.com/docker/metadata-action#bake-definition
target "meta-helper" {
  tags = ["docker/buildx-bin:local"]
}

target "_common" {
  args = {
    GO_VERSION = GO_VERSION
    BUILDKIT_CONTEXT_KEEP_GIT_DIR = 1
  }
}

group "default" {
  targets = ["binaries"]
}

group "validate" {
  targets = ["lint", "lint-gopls", "validate-golangci", "validate-vendor", "validate-docs"]
}

target "lint" {
  inherits = ["_common"]
  dockerfile = "./hack/dockerfiles/lint.Dockerfile"
  output = ["type=cacheonly"]
  platforms = GOLANGCI_LINT_MULTIPLATFORM != "" ? [
    "darwin/amd64",
    "darwin/arm64",
    "freebsd/amd64",
    "freebsd/arm64",
    "linux/amd64",
    "linux/arm64",
    "linux/s390x",
    "linux/ppc64le",
    "linux/riscv64",
    "openbsd/amd64",
    "openbsd/arm64",
    "windows/amd64",
    "windows/arm64"
  ] : []
}

target "validate-golangci" {
  description = "Validate .golangci.yml schema (does not run Go linter)"
  inherits = ["_common"]
  dockerfile = "./hack/dockerfiles/lint.Dockerfile"
  target = "validate-golangci"
  output = ["type=cacheonly"]
}

target "lint-gopls" {
  inherits = ["lint"]
  target = "gopls-analyze"
}

target "validate-vendor" {
  inherits = ["_common"]
  dockerfile = "./hack/dockerfiles/vendor.Dockerfile"
  target = "validate"
  output = ["type=cacheonly"]
}

target "validate-docs" {
  inherits = ["_common"]
  args = {
    FORMATS = DOCS_FORMATS
    BUILDX_EXPERIMENTAL = 1 // enables experimental cmds/flags for docs generation
  }
  dockerfile = "./hack/dockerfiles/docs.Dockerfile"
  target = "validate"
  output = ["type=cacheonly"]
}

target "validate-authors" {
  inherits = ["_common"]
  dockerfile = "./hack/dockerfiles/authors.Dockerfile"
  target = "validate"
  output = ["type=cacheonly"]
}

target "validate-generated-files" {
  inherits = ["_common"]
  dockerfile = "./hack/dockerfiles/generated-files.Dockerfile"
  target = "validate"
  output = ["type=cacheonly"]
}

target "update-vendor" {
  inherits = ["_common"]
  dockerfile = "./hack/dockerfiles/vendor.Dockerfile"
  target = "update"
  output = ["."]
}

target "update-docs" {
  inherits = ["_common"]
  args = {
    FORMATS = DOCS_FORMATS
    BUILDX_EXPERIMENTAL = 1 // enables experimental cmds/flags for docs generation
  }
  dockerfile = "./hack/dockerfiles/docs.Dockerfile"
  target = "update"
  output = ["./docs/reference"]
}

target "update-authors" {
  inherits = ["_common"]
  dockerfile = "./hack/dockerfiles/authors.Dockerfile"
  target = "update"
  output = ["."]
}

target "update-generated-files" {
  inherits = ["_common"]
  dockerfile = "./hack/dockerfiles/generated-files.Dockerfile"
  target = "update"
  output = ["."]
}

target "mod-outdated" {
  inherits = ["_common"]
  dockerfile = "./hack/dockerfiles/vendor.Dockerfile"
  target = "outdated"
  no-cache-filter = ["outdated"]
  output = ["type=cacheonly"]
}

target "test" {
  inherits = ["_common"]
  target = "test-coverage"
  output = ["${DESTDIR}/coverage"]
}

target "binaries" {
  inherits = ["_common"]
  target = "binaries"
  output = ["${DESTDIR}/build"]
  platforms = ["local"]
}

target "binaries-cross" {
  inherits = ["binaries"]
  platforms = [
    "darwin/amd64",
    "darwin/arm64",
    "freebsd/amd64",
    "freebsd/arm64",
    "linux/amd64",
    "linux/arm/v6",
    "linux/arm/v7",
    "linux/arm64",
    "linux/ppc64le",
    "linux/riscv64",
    "linux/s390x",
    "openbsd/amd64",
    "openbsd/arm64",
    "windows/amd64",
    "windows/arm64"
  ]
}

target "release" {
  inherits = ["binaries-cross"]
  target = "release"
  output = ["${DESTDIR}/release"]
}

target "image" {
  inherits = ["meta-helper", "binaries"]
  output = ["type=image"]
}

target "image-cross" {
  inherits = ["meta-helper", "binaries-cross"]
  output = ["type=image"]
}

target "image-local" {
  inherits = ["image"]
  output = ["type=docker"]
}

variable "HTTP_PROXY" {
  default = ""
}
variable "HTTPS_PROXY" {
  default = ""
}
variable "NO_PROXY" {
  default = ""
}
variable "TEST_BUILDKIT_TAG" {
  default = null
}

target "integration-test-base" {
  inherits = ["_common"]
  args = {
    GO_EXTRA_FLAGS = TEST_COVERAGE == "1" ? "-cover" : null
    HTTP_PROXY = HTTP_PROXY
    HTTPS_PROXY = HTTPS_PROXY
    NO_PROXY = NO_PROXY
    BUILDKIT_VERSION = TEST_BUILDKIT_TAG
  }
  target = "integration-test-base"
  output = ["type=cacheonly"]
}

target "integration-test" {
  inherits = ["integration-test-base"]
  target = "integration-test"
}

variable "GOVULNCHECK_FORMAT" {
  default = null
}

target "govulncheck" {
  inherits = ["_common"]
  dockerfile = "./hack/dockerfiles/govulncheck.Dockerfile"
  target = "output"
  args = {
    FORMAT = GOVULNCHECK_FORMAT
  }
  no-cache-filter = ["run"]
  output = ["${DESTDIR}"]
}
