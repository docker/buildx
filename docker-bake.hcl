variable "GO_VERSION" {
  default = "1.19"
}
variable "DOCS_FORMATS" {
  default = "md"
}
variable "DESTDIR" {
  default = "./bin"
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
  targets = ["lint", "validate-vendor", "validate-docs"]
}

target "lint" {
  inherits = ["_common"]
  dockerfile = "./hack/dockerfiles/lint.Dockerfile"
  output = ["type=cacheonly"]
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
    "linux/amd64",
    "linux/arm/v6",
    "linux/arm/v7",
    "linux/arm64",
    "linux/ppc64le",
    "linux/riscv64",
    "linux/s390x",
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
