package client

import (
	"encoding/json"
	"net/http"

	"github.com/pkg/errors"
	httperror "github.com/portainer/portainer/pkg/libhttp/error"
	"github.com/rs/zerolog/log"
)

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
