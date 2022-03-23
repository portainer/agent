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

	log.Printf("ServerURL=%s", serverURL)

	resp, err := http.Get("http://localhost:9005/lookup?serverurl=" + serverURL)
	if err != nil {
		// TODO: probably shouldn't do this
		log.Fatalln(err)
	}

	// body, err := ioutil.ReadAll(resp.Body)
	// if err != nil {
	// 	log.Fatalln(err)
	// }

	//Convert the body to type string
	//log.Printf("Body:", string(body))

	var c credentials.Credentials

	err = json.NewDecoder(resp.Body).Decode(&c)
	if err != nil {
		log.Printf("Get failed %v\n", err)
		return "", "", credentials.NewErrCredentialsNotFound()
	}

	log.Printf("Success credentials: %+v\n", c)

	return c.Username, c.Secret, nil
	//	return "", "", credentials.NewErrCredentialsNotFound()
}

func (h *portainerHelper) List() (map[string]string, error) {
	return nil, nil
}
