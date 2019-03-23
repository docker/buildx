shell:
	./hack/shell

binaries:
	./hack/binaries

install: binaries
	mkdir -p ~/.docker/cli-plugins
	cp bin/buildx ~/.docker/cli-plugins/docker-buildx

lint:
	./hack/lint

vendor:
	./hack/update-vendor

.PHONY: vendor lint shell binaries install