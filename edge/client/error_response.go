package client

import (
	"encoding/json"
	"net/http"

	"github.com/rs/zerolog/log"
)

func logError(resp *http.Response) {
	var errorData struct {
		Details string
		Message string
	}

	err := json.NewDecoder(resp.Body).Decode(&errorData)
	if err != nil {
		log.Debug().Err(err).Msg("Failed to decode error response")
		return
	}
	log.Debug().Str("error_response_message", errorData.Details).Str("error_response_details", errorData.Details).Int("status_code", resp.StatusCode).Msg("poll request failure]")

	return
}
