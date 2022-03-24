package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"

	credentials "github.com/docker/docker-credential-helpers/credentials"
)

type portainerHelper struct {
}

func (h *portainerHelper) Add(*credentials.Credentials) error {
	return nil
}

func (h *portainerHelper) Delete(serverURL string) error {
	return nil
}

func (h *portainerHelper) Get(serverURL string) (string, string, error) {
	f, err := os.OpenFile("/tmp/portainer-credential-helper.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("error opening file: %v", err)
	}
	defer f.Close()
	log.SetOutput(f)

	if serverURL == "" {
		return "", "", credentials.NewErrCredentialsMissingServerURL()
	}

	log.Printf("GET ServerURL=%s", serverURL)

	resp, err := http.Get("http://localhost:9005/lookup?serverurl=" + serverURL)
	if err != nil {
		log.Printf("Error getting credentials: %v", err)
		return "", "", credentials.NewErrCredentialsNotFound()
	}

	var c credentials.Credentials

	err = json.NewDecoder(resp.Body).Decode(&c)
	if err != nil {
		log.Printf("Invalid response: %v\n", err)
		return "", "", credentials.NewErrCredentialsNotFound()
	}

	secret := c.Secret
	c.Secret = "[REDACTED]"
	log.Printf("Lookup Succeeded. Credentials: %+v", c)

	return c.Username, secret, nil
}

func (h *portainerHelper) List() (map[string]string, error) {
	return nil, nil
}
