# See: https://gist.github.com/asukakenji/f15ba7e588ac42795f421b48b8aede63
# For a list of valid GOOS and GOARCH values
# Note: these can be overriden on the command line e.g. `make PLATFORM=<platform> ARCH=<arch>`
PLATFORM=$(shell go env GOOS)
ARCH=$(shell go env GOARCH)

ifeq ("$(PLATFORM)", "windows")
agent=agent.exe
docker-credential-portainer=docker-credential-portainer.exe
else
agent=agent
docker-credential-portainer=docker-credential-portainer
endif

.PHONY: $(agent) $(docker-credential-portainer) download-binaries clean help

all: $(agent) $(docker-credential-portainer) download-binaries ## Build everything

agent: ## Build the agent
	@echo "Building agent..."
	@CGO_ENABLED=0 GOOS=$(PLATFORM) GOARCH=$(ARCH) go build -trimpath --installsuffix cgo --ldflags "-s" -o dist/$(agent) cmd/agent/main.go

docker-credential-portainer: ## Build the credential helper used by edge private registries
	@echo "Building docker-credential-portainer..."
	@cd cmd/docker-credential-portainer; \
	CGO_ENABLED=0 GOOS=$(PLATFORM) GOARCH=$(ARCH) go build -trimpath --installsuffix cgo --ldflags "-s" -o ../../dist/$(docker-credential-portainer)

download-binaries: ## Download dependant binaries
	@./setup.sh $(PLATFORM) $(ARCH)

clean: ## Remove all build and download artifacts
	@rm -f dist/*

help: 
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'
