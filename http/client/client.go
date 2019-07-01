package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/pkg/errors"
)

// Client is used to execute HTTP requests against the agent API
type Client struct {
	httpClient *http.Client
}

// NewClient returns a pointer to a new Client instance
func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: time.Second * 3,
		},
	}
}

type getEdgeKeyResponse struct {
	Key string `json:"key"`
}

// TODO: doc
func (client *Client) GetEdgeKey(serverAddr string) (string, error) {
	requestURL := fmt.Sprintf("http://%s/key", serverAddr)

	req, err := http.NewRequest(http.MethodGet, requestURL, nil)
	if err != nil {
		return "", err
	}

	resp, err := client.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("[ERROR] [http,client] [response_code: %d] [message: GetEdgeKey operation failed]", resp.StatusCode)
		return "", errors.New("GetEdgeKey operation failed")
	}

	var data getEdgeKeyResponse
	err = json.NewDecoder(resp.Body).Decode(&data)
	if err != nil {
		return "", err
	}

	return data.Key, nil
}

type setEdgeKeyPayload struct {
	Key string
}

// TODO: doc
func (client *Client) SetEdgeKey(serverAddr, key string) error {
	payload := setEdgeKeyPayload{
		Key: key,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	requestURL := fmt.Sprintf("http://%s/key", serverAddr)

	req, err := http.NewRequest(http.MethodPost, requestURL, bytes.NewReader(data))
	if err != nil {
		return err
	}

	resp, err := client.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		log.Printf("[ERROR] [http,client] [response_code: %d] [message: SetEdgeKey operation failed]", resp.StatusCode)
	}

	return nil
}
