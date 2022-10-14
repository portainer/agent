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

.PHONY: $(agent) $(docker-credential-portainer) download-binaries clean

all: $(agent) $(docker-credential-portainer) download-binaries

$(agent):
	@echo "Building agent..."
	@CGO_ENABLED=0 GOOS=$(PLATFORM) GOARCH=$(ARCH) go build -trimpath --installsuffix cgo --ldflags "-s" -o dist/$@ cmd/agent/main.go

$(docker-credential-portainer):
	@echo "Building docker-credential-portainer..."
	@cd cmd/docker-credential-portainer; \
	CGO_ENABLED=0 GOOS=$(PLATFORM) GOARCH=$(ARCH) go build -trimpath --installsuffix cgo --ldflags "-s" -o ../../dist/$@

download-binaries:
	@./setup.sh $(PLATFORM) $(ARCH)

clean:
	@rm -f dist/*

