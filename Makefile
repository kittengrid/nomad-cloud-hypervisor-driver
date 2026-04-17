PLUGIN_BINARY := cloud-hypervisor-driver
export GO111MODULE := on

default: build

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf $(PLUGIN_BINARY)

.PHONY: build
build:
	go build -o $(PLUGIN_BINARY) .

.PHONY: test
test:
	@echo "==> Run unit tests"
	go test -race -v ./...

.PHONY: e2e
e2e: build
	@echo "==> Run e2e tests"
	go test -v -tags=e2e ./internal/oci ./e2e
