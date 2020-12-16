package dockerhub

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	httperror "github.com/portainer/libhttp/error"
	"github.com/portainer/libhttp/request"
	"github.com/portainer/libhttp/response"
)

type dockerhubStatusPayload struct {
	Authentication bool   `json:"authentication"`
	Username       string `json:"username"`
	Password       string `json:"password"`
}

func (payload *dockerhubStatusPayload) Validate(r *http.Request) error {
	if payload.Authentication {
		if payload.Username == "" || payload.Password == "" {
			return errors.New("Missing username or password")
		}
	}

	return nil
}

type dockerhubStatusResponse struct {
	Remaining int `json:"remaining"`
	Limit     int `json:"limit"`
}

// GET request on /api/endpoints/{id}/dockerhub/status
func (handler *Handler) dockerhubStatus(w http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	var payload dockerhubStatusPayload
	err := request.DecodeAndValidateJSONPayload(r, &payload)
	if err != nil {
		return &httperror.HandlerError{http.StatusBadRequest, "Invalid request payload", err}
	}

	httpClient := &http.Client{
		Timeout: time.Second * 3,
	}
	token, err := getDockerHubToken(httpClient, &payload)
	if err != nil {
		return &httperror.HandlerError{http.StatusInternalServerError, "Unable to retrieve DockerHub token from DockerHub", err}
	}

	resp, err := getDockerHubLimits(httpClient, token)
	if err != nil {
		return &httperror.HandlerError{http.StatusInternalServerError, "Unable to retrieve DockerHub rate limits from DockerHub", err}
	}

	return response.JSON(w, resp)
}

func getDockerHubToken(httpClient *http.Client, dockerhub *dockerhubStatusPayload) (string, error) {
	type dockerhubTokenResponse struct {
		Token string `json:"token"`
	}

	requestURL := "https://auth.docker.io/token?service=registry.docker.io&scope=repository:ratelimitpreview/test:pull"

	req, err := http.NewRequest(http.MethodGet, requestURL, nil)
	if err != nil {
		return "", err
	}

	if dockerhub.Authentication {
		req.SetBasicAuth(dockerhub.Username, dockerhub.Password)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", errors.New("failed fetching dockerhub token")
	}

	var data dockerhubTokenResponse
	err = json.NewDecoder(resp.Body).Decode(&data)
	if err != nil {
		return "", err
	}

	return data.Token, nil
}

func getDockerHubLimits(httpClient *http.Client, token string) (*dockerhubStatusResponse, error) {

	requestURL := "https://registry-1.docker.io/v2/ratelimitpreview/test/manifests/latest"

	req, err := http.NewRequest(http.MethodHead, requestURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", token))

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("failed fetching dockerhub limits")
	}

	rateLimit, err := parseRateLimitHeader(resp.Header, "RateLimit-Limit")
	rateLimitRemaining, err := parseRateLimitHeader(resp.Header, "RateLimit-Remaining")

	return &dockerhubStatusResponse{
		Limit:     rateLimit,
		Remaining: rateLimitRemaining,
	}, nil
}

func parseRateLimitHeader(headers http.Header, headerKey string) (int, error) {
	headerValue := headers.Get(headerKey)
	if headerValue == "" {
		return 0, fmt.Errorf("Missing %s header", headerKey)
	}

	matches := strings.Split(headerValue, ";")
	value, err := strconv.Atoi(matches[0])
	if err != nil {
		return 0, err
	}

	return value, nil
}
