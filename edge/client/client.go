package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
)

// APIClient is used to execute HTTP requests against the agent API
type APIClient struct {
	httpClient *http.Client
}

// NewAPIClient returns a pointer to a new APIClient instance
func NewAPIClient() *APIClient {
	return &APIClient{
		httpClient: &http.Client{
			Timeout: time.Second * 3,
		},
	}
}

type getEdgeKeyResponse struct {
	Key string `json:"key"`
}

// GetEdgeKey executes a KeyInspect operation against the specified server
func (client *APIClient) GetEdgeKey(serverAddr string) (string, error) {
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
		log.Error().Int("response_code", resp.StatusCode).Msg("GetEdgeKey operation failed")

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

// SetEdgeKey executes a KeyCreate operation against the specified server
func (client *APIClient) SetEdgeKey(serverAddr, key string) error {
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
		log.Error().Int("response_code", resp.StatusCode).Msg("SetEdgeKey operation failed")
	}

	return nil
}
