shell:
	./hack/shell

lint:
	./hack/lint
	
vendor:
	./hack/update-vendor

.PHONY: vendor lint shell