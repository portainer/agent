package browse

import (
	"errors"
	"net/http"

	"github.com/portainer/agent/filesystem"
	httperror "github.com/portainer/libhttp/error"
	"github.com/portainer/libhttp/request"
	"github.com/portainer/libhttp/response"
)

type browsePutPayload struct {
	Path     string
	Filename string
	File     []byte
}

func (payload *browsePutPayload) Validate(r *http.Request) error {
	path, err := request.RetrieveMultiPartFormValue(r, "Path", false)
	if err != nil {
		return errors.New("invalid file path")
	}
	payload.Path = path

	file, filename, err := request.RetrieveMultiPartFormFile(r, "file")
	if err != nil {
		return errors.New("invalid uploaded file")
	}
	payload.File = file
	payload.Filename = filename

	return nil
}

// POST request on /browse/put?volumeID=:id
func (handler *Handler) browsePut(rw http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	var payload browsePutPayload

	values := r.URL.Query()
	volumeID := values.Get("volumeID")

	file, fileheader, err := r.FormFile("file")
	if err != nil {
		httperror.BadRequest("Invalid request payload", err)
	}
	defer file.Close()

	if vs := r.Form["Path"]; len(vs) > 0 {
		payload.Path = vs[0]
	} else {
		return httperror.BadRequest("Invalid request payload", errors.New("invalid file path"))
	}

	if volumeID != "" {
		payload.Path, err = filesystem.BuildPathToFileInsideVolume(volumeID, payload.Path)
		if err != nil {
			return httperror.BadRequest("Invalid volume", err)
		}

		_, err = filesystem.BuildPathToFileInsideVolume(volumeID, payload.Filename)
		if err != nil {
			return httperror.BadRequest("Invalid filename", err)
		}
	}

	err = filesystem.WriteBigFile(payload.Path, fileheader.Filename, file, 0755)
	if err != nil {
		return httperror.InternalServerError("Error saving file to disk", err)
	}

	return response.Empty(rw)
}

// POST request on /v1/browse/:id/put
func (handler *Handler) browsePutV1(rw http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	volumeID, err := request.RetrieveRouteVariableValue(r, "id")
	if err != nil {
		return httperror.BadRequest("Invalid volume identifier route variable", err)
	}

	var payload browsePutPayload
	err = payload.Validate(r)
	if err != nil {
		return httperror.BadRequest("Invalid request payload", err)
	}
	payload.Path, err = filesystem.BuildPathToFileInsideVolume(volumeID, payload.Path)
	if err != nil {
		return httperror.BadRequest("Invalid volume", err)
	}

	err = filesystem.WriteFile(payload.Path, payload.Filename, payload.File, 0755)
	if err != nil {
		return httperror.InternalServerError("Error saving file to disk", err)
	}

	return response.Empty(rw)
}
