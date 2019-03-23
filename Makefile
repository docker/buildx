shell:
	./hack/shell

binaries:
	./hack/binaries

lint:
	./hack/lint
	
vendor:
	./hack/update-vendor

.PHONY: vendor lint shell binaries