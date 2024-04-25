ifneq (, $(BUILDX_BIN))
	export BUILDX_CMD = $(BUILDX_BIN)
else ifneq (, $(shell docker buildx version))
	export BUILDX_CMD = docker buildx
else ifneq (, $(shell which buildx))
	export BUILDX_CMD = $(which buildx)
endif

export BUILDX_CMD ?= docker buildx

BAKE_TARGETS := binaries binaries-cross lint lint-gopls validate-vendor validate-docs validate-authors validate-generated-files

.PHONY: all
all: binaries

.PHONY: build
build:
	./hack/build

.PHONY: shell
shell:
	./hack/shell

.PHONY: $(BAKE_TARGETS)
$(BAKE_TARGETS):
	$(BUILDX_CMD) bake $@

.PHONY: install
install: binaries
	mkdir -p ~/.docker/cli-plugins
	install bin/build/buildx ~/.docker/cli-plugins/docker-buildx

.PHONY: release
release:
	./hack/release

.PHONY: validate-all
validate-all: lint test validate-vendor validate-docs validate-generated-files

.PHONY: test
test:
	./hack/test

.PHONY: test-unit
test-unit:
	TESTPKGS=./... SKIP_INTEGRATION_TESTS=1 ./hack/test

.PHONY: test
test-integration:
	TESTPKGS=./tests ./hack/test

.PHONY: test-driver
test-driver:
	./hack/test-driver

.PHONY: vendor
vendor:
	./hack/update-vendor

.PHONY: docs
docs:
	./hack/update-docs

.PHONY: authors
authors:
	$(BUILDX_CMD) bake update-authors

.PHONY: mod-outdated
mod-outdated:
	$(BUILDX_CMD) bake mod-outdated

.PHONY: generated-files
generated-files:
	$(BUILDX_CMD) bake update-generated-files
