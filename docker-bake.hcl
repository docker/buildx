variable "GO_VERSION" {
  default = "1.17"
}
variable "BIN_OUT" {
  default = "./bin"
}
variable "RELEASE_OUT" {
  default = "./release-out"
}
variable "DOCS_FORMATS" {
  default = "md"
}

// Special target: https://github.com/docker/metadata-action#bake-definition
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
  args = {
    // used to invalidate cache for outdated run stage
    // can be dropped when https://github.com/moby/buildkit/issues/1213 fixed
    _RANDOM = uuidv4()
  }
  output = ["type=cacheonly"]
}

target "test" {
  inherits = ["_common"]
  target = "test-coverage"
  output = ["./coverage"]
}

target "binaries" {
  inherits = ["_common"]
  target = "binaries"
  output = [BIN_OUT]
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
  output = [RELEASE_OUT]
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

# Sets the list of package types to build: apk, deb, rpm and/or static
variable "PKG_TYPES" {
  default = "apk,deb,rpm,static"
}
# Sets the list of apk releases (e.g., r0)
# docker-buildx-plugin_0.8.1-r0_aarch64.apk
variable "PKG_APK_RELEASES" {
  default = "r0"
}
# Sets the list of deb releases (e.g., debian11)
# docker-buildx-plugin_0.8.1-debian11_arm64.deb
variable "PKG_DEB_RELEASES" {
  default = "debian10,debian11,ubuntu1804,ubuntu2004,ubuntu2110,ubuntu2204,raspbian10,raspbian11"
}
# Sets the list of rpm releases (e.g., centos7)
# docker-buildx-plugin-0.8.1-fedora35.aarch64.rpm
variable "PKG_RPM_RELEASES" {
  default = "centos7,centos8,fedora33,fedora34,fedora35,fedora36"
}
# Sets the vendor/maintainer name (only for linux packages)
variable "PKG_VENDOR" {
  default = "Docker"
}
# Sets the name of the company that produced the package (only for linux packages)
variable "PKG_PACKAGER" {
  default = "Docker <support@docker.com>"
}

# Useful commands for this target
# PKG_TYPES=deb PKG_DEB_RELEASES=debian11 docker buildx bake pkg
# docker buildx bake --set *.platform=windows/amd64 --set *.output=./bin pkg
target "pkg" {
  inherits = ["binaries"]
  args = {
    PKG_TYPES = PKG_TYPES
    PKG_APK_RELEASES = PKG_APK_RELEASES
    PKG_DEB_RELEASES = PKG_DEB_RELEASES
    PKG_RPM_RELEASES = PKG_RPM_RELEASES
    PKG_VENDOR = PKG_VENDOR
    PKG_PACKAGER = PKG_PACKAGER
  }
  target = "pkg"
}

# Useful commands for this target
# docker buildx bake pkg-cross --set *.output=type=image,push=true --set *.tags=crazymax/buildx-pkg:latest
target "pkg-cross" {
  inherits = ["pkg"]
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
