package main

import (
	credentials "github.com/docker/docker-credential-helpers/credentials"
)

func main() {
	helper := &portainerHelper{}
	credentials.Serve(helper)
}
