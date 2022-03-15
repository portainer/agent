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
	// TODO: Remove this logging later
	f, err := os.OpenFile("/tmp/portainer-credential-helper.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("error opening file: %v", err)
	}
	defer f.Close()
	log.SetOutput(f)

	if serverURL == "" {
		return "", "", credentials.NewErrCredentialsMissingServerURL()
	}

	log.Println("Get: server=", serverURL)

	resp, err := http.Get("http://localhost:9001/registry?serverurl=" + serverURL)
	if err != nil {
		// TODO: probably shouldn't do this
		log.Fatalln(err)
	}

	var c credentials.Credentials

	err = json.NewDecoder(resp.Body).Decode(&c)
	if err != nil {
		log.Printf("Get failed %v\n", err)
		return "", "", credentials.NewErrCredentialsNotFound()
	}

	log.Printf("Success: %+v\n", c)

	return c.Username, c.Secret, nil
}

func (h *portainerHelper) List() (map[string]string, error) {
	return nil, nil
}
