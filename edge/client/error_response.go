package client

import (
	"encoding/json"
	"errors"
	"net/http"

	httperror "github.com/portainer/portainer/pkg/libhttp/error"
	"github.com/rs/zerolog/log"
)

func logPollingError(resp *http.Response, ctxMsg, errMsg string) error {
	var respErr httperror.HandlerError
	if err := json.NewDecoder(resp.Body).Decode(&respErr); err != nil {
		log.
			Error().
			Err(err).
			Str("context", ctxMsg).
			Int("response_code", resp.StatusCode).
			Msg("PollClient failed to decode server response")
	}
	log.
		Error().Err(respErr.Err).
		Str("context", ctxMsg).
		Str("response message", respErr.Message).
		Int("status code", respErr.StatusCode).
		Msg(errMsg)
	return errors.New(errMsg)
}
