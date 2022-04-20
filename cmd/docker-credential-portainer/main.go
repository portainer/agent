package main

import (
	"log"
	"os"

	credentials "github.com/docker/docker-credential-helpers/credentials"
)

func main() {
	f, err := os.OpenFile("/tmp/portainer-credential-helper.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("error opening file: %v", err)
	}
	defer f.Close()
	log.SetOutput(f)

	log.Printf("running portainer-credential-helper")

	helper := &portainerHelper{}
	credentials.Serve(helper)
}
