ifneq (, $(BUILDX_BIN))
	export BUILDX_CMD = $(BUILDX_BIN)
else ifneq (, $(shell docker buildx version))
	export BUILDX_CMD = docker buildx
else ifneq (, $(shell which buildx))
	export BUILDX_CMD = $(which buildx)
endif

export BUILDX_CMD ?= docker buildx

.PHONY: all
all: binaries

.PHONY: build
build:
	./hack/build

.PHONY: shell
shell:
	./hack/shell

.PHONY: binaries
binaries:
	$(BUILDX_CMD) bake binaries

.PHONY: binaries-cross
binaries-cross:
	$(BUILDX_CMD) bake binaries-cross

.PHONY: install
install: binaries
	mkdir -p ~/.docker/cli-plugins
	install bin/build/buildx ~/.docker/cli-plugins/docker-buildx

.PHONY: release
release:
	./hack/release

.PHONY: validate-all
validate-all: lint test validate-vendor validate-docs validate-generated-files

.PHONY: lint
lint:
	$(BUILDX_CMD) bake lint

.PHONY: test
test:
	$(BUILDX_CMD) bake test

.PHONY: validate-vendor
validate-vendor:
	$(BUILDX_CMD) bake validate-vendor

.PHONY: validate-docs
validate-docs:
	$(BUILDX_CMD) bake validate-docs

.PHONY: validate-authors
validate-authors:
	$(BUILDX_CMD) bake validate-authors

.PHONY: validate-generated-files
validate-generated-files:
	$(BUILDX_CMD) bake validate-generated-files

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
