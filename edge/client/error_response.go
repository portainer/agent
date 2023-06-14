package client

import (
	"encoding/json"
	"net/http"

	"github.com/rs/zerolog/log"
)

type errorData struct {
	Details string
	Message string
}

func parseError(resp *http.Response) *errorData {
	errorData := &errorData{}

	err := json.NewDecoder(resp.Body).Decode(&errorData)
	if err != nil {
		log.Debug().CallerSkipFrame(1).
			Err(err).
			Int("status_code", resp.StatusCode).
			Msg("failed to decode error response")

		return nil
	}

	return errorData
}

func logError(resp *http.Response, errorData *errorData) {
	if errorData == nil {
		return
	}

	log.Debug().CallerSkipFrame(1).
		Str("error_response_message", errorData.Message).
		Str("error_response_details", errorData.Details).
		Int("status_code", resp.StatusCode).
		Msg("poll request failure")
}
