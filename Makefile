PLATFORM="linux"
ARCH="amd64"

all: agent docker-credential-portainer binaries

agent:
	@echo "Building agent..."
	@GOOS=$(PLATFORM) GOARCH=$(ARCH) CGO_ENABLED=0 go build --installsuffix cgo --ldflags '-s'  -o dist/$@ cmd/agent/main.go

docker-credential-portainer:
	@echo "Building docker-credential-portainer..."
	@cd cmd/docker-credential-portainer; \
	GOOS=$(PLATFORM) GOARCH=$(ARCH) CGO_ENABLED=0 go build --installsuffix cgo --ldflags '-s' -o ../../dist/$@

binaries:
	@./setup.sh $(PLATFORM) $(ARCH)

clean:
	rm -f dist/*