package client

import (
	"encoding/json"
	"net/http"

	httperror "github.com/portainer/portainer/pkg/libhttp/error"
	"github.com/rs/zerolog/log"
)

type NonOkResponseError struct {
	msg string
}

func newNonOkResponseError(msg string) *NonOkResponseError {
	return &NonOkResponseError{msg: msg}
}

func (e *NonOkResponseError) Error() string {
	return e.msg
}

type ForbiddenResponseError struct {
	msg string
}

func newForbiddenResponseError(msg string) *ForbiddenResponseError {
	return &ForbiddenResponseError{msg: msg}
}

func (e *ForbiddenResponseError) Error() string {
	return e.msg
}

func decodeNonOkayResponse(resp *http.Response, ctxMsg string) *httperror.HandlerError {
	var respErr httperror.HandlerError
	if err := json.NewDecoder(resp.Body).Decode(&respErr); err != nil {
		log.
			Error().
			Err(err).
			CallerSkipFrame(1).
			Str("context", ctxMsg).
			Int("response_code", resp.StatusCode).
			Msg("PollClient failed to decode server response")
		return nil
	}
	return &respErr
}
