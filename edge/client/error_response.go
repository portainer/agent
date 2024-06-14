package client

import (
	"encoding/json"
	"net/http"

	"github.com/pkg/errors"
	httperror "github.com/portainer/portainer/pkg/libhttp/error"
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

func logPollingError(resp *http.Response, errMsg string) error {
	var respErr httperror.HandlerError
	if err := json.NewDecoder(resp.Body).Decode(&respErr); err != nil {
		log.Error().Err(err).Int("response_code", resp.StatusCode).Msg("failed to parse response error")
	}
	log.Error().Err(respErr.Err).
		Str("response message", respErr.Message).
		Int("status code", respErr.StatusCode).
		Msg(errMsg)
	return errors.New(errMsg)
}
