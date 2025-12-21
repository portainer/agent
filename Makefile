# See: https://gist.github.com/asukakenji/f15ba7e588ac42795f421b48b8aede63
# For a list of valid GOOS and GOARCH values
# Note: these can be overriden on the command line e.g. `make PLATFORM=<platform> ARCH=<arch>`
PLATFORM=$(shell go env GOOS)
ARCH=$(shell go env GOARCH)

GOTESTSUM=go run gotest.tools/gotestsum@latest

ifeq ("$(PLATFORM)", "windows")
agent=agent.exe
credential-helper=docker-credential-portainer.exe
healthy=healthy.exe
else
agent=agent
credential-helper=docker-credential-portainer
healthy=healthy
endif

.DEFAULT_GOAL := help
.PHONY: agent credential-helper healthy download-binaries clean help

##@ Building

all: tidy credential-helper healthy download-binaries mock agent ## Build everything

agent: ## Build the agent
	@echo "Building Portainer agent..."
	@CGO_ENABLED=0 GOOS=$(PLATFORM) GOARCH=$(ARCH) go build -trimpath --installsuffix cgo --ldflags "-s" -o dist/$(agent) ./cmd/agent/

credential-helper: ## Build the credential helper (used by edge private registries)
	@if [ ! -f dist/$(credential-helper) ]; then \
		echo "Building Portainer credential-helper..."; \
		cd cmd/docker-credential-portainer && \
		CGO_ENABLED=0 GOOS=$(PLATFORM) GOARCH=$(ARCH) go build -trimpath --installsuffix cgo --ldflags "-s" -o ../../dist/$(credential-helper); \
	else \
		echo "Credential helper already exists, skipping build."; \
	fi

healthy: ## Build the healthy binary
	@if [ ! -f dist/$(healthy) ]; then \
		echo "Building healthy..."; \
		cd cmd/healthy && \
		CGO_ENABLED=0 GOOS=$(PLATFORM) GOARCH=$(ARCH) go build -trimpath --installsuffix cgo --ldflags "-s" -o ../../dist/$(healthy); \
	else \
		echo "healthy already exists, skipping build."; \
	fi

download-binaries: ## Download dependant binaries
	@./setup.sh $(PLATFORM) $(ARCH)

image: ## Build the agent and the image
	@./dev.sh build -c

##@ Dependencies

tidy: ## Tidy up the go.mod file
	@go mod tidy

##@ Testing
.PHONY: test test-client test-server

test:	## Run server tests
	$(GOTESTSUM) --format pkgname-and-test-fails --format-hide-empty-pkg --hide-summary skipped -- -cover -race -covermode=atomic -coverprofile=coverage.out ./...

##@ Miscellaneous

lint:   ## Run linter
	go mod tidy
	golangci-lint run -c .golangci.yaml

clean: ## Remove all build and download artifacts
	@echo "Clearing the dist directory..."
	@rm -f dist/*

mock: ## Regenerate the internals/mocks/* files | DL = go install go.uber.org/mock/mockgen@latest
	@go install go.uber.org/mock/mockgen@latest
	mockgen -package mocks -source=./agent.go -destination=./internals/mocks/mock_agent.go
	mockgen -package mocks -source=./edge/client/interface.go -destination=./internals/mocks/mock_edge.go
	mockgen -package mocks -source=./deployer/interface.go -destination=./internals/mocks/mock_deployer.go

##@ Helpers

help:  ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)
