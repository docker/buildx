PKG=github.com/tonistiigi/buildx
VERSION=$(shell git describe --match 'v[0-9]*' --dirty='.m' --always --tags)
REVISION=$(shell git rev-parse HEAD)$(shell if ! git diff --no-ext-diff --quiet --exit-code; then echo .m; fi)
LDFLAGS=-X ${PKG}/version.Version=${VERSION} \
	-X ${PKG}/version.Revision=${REVISION} \
	-X ${PKG}/version.Package=${PKG}

GOFILES=$(shell find . -type f -name '*.go')

.PHONY: build
build: plugin

bin/buildx bin/docker-buildx: $(GOFILES)
	go build -ldflags "$(LDFLAGS)" -o $@ ./cmd/buildx

.PHONY: clean
clean:
	$(RM) -r bin/

.PHONY: standalone
standalone: bin/buildx

.PHONY: plugin
plugin: bin/docker-buildx

shell:
	./hack/shell

binaries:
	./hack/binaries

binaries-cross:
	EXPORT_LOCAL=cross-out ./hack/cross

install: binaries
	mkdir -p ~/.docker/cli-plugins
	cp bin/buildx ~/.docker/cli-plugins/docker-buildx

lint:
	./hack/lint

test:
	./hack/test

validate-vendor:
	./hack/validate-vendor

validate-all: lint test validate-vendor

vendor:
	./hack/update-vendor

.PHONY: vendor lint shell binaries install binaries-cross validate-all
