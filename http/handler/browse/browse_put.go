package browse

import (
	"bufio"
	"errors"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path"
	"strconv"

	"github.com/portainer/agent/filesystem"
	httperror "github.com/portainer/libhttp/error"
	"github.com/portainer/libhttp/request"
	"github.com/portainer/libhttp/response"
)

type browsePutPayload struct {
	Path     string
	Filename string
	File     []byte
	Header   *multipart.FileHeader
}

func (payload *browsePutPayload) Validate(r *http.Request) error {
	path, err := request.RetrieveMultiPartFormValue(r, "Path", false)
	if err != nil {
		return errors.New("Invalid file path")
	}
	payload.Path = path

	file, filename, err := request.RetrieveMultiPartFormFile(r, "file")
	if err != nil {
		return errors.New("Invalid uploaded file")
	}
	payload.File = file
	payload.Filename = filename

	return nil
}

// POST request on /browse/put?volumeID=:id
func (handler *Handler) browsePut(rw http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	volumeID, err := request.RetrieveQueryParameter(r, "volumeID", true)
	if volumeID == "" && !handler.agentOptions.HostManagementEnabled {
		return &httperror.HandlerError{http.StatusServiceUnavailable, "Host management capability disabled", errors.New("This agent feature is not enabled")}
	}

	var payload browsePutPayload
	payload.Path, err = request.RetrieveMultiPartFormValue(r, "Path", false)
	if err != nil {
		return &httperror.HandlerError{http.StatusBadRequest, "Invalid request path", err}
	}

	err = r.ParseMultipartForm(32 << 20)
	if err != nil {
		return &httperror.HandlerError{http.StatusBadRequest, "Invalid request payload", err}
	}
	if r.MultipartForm != nil && r.MultipartForm.File != nil {
		if fhs := r.MultipartForm.File["file"]; len(fhs) > 0 {
			payload.Header = fhs[0]
			payload.Filename = fhs[0].Filename
		}
	}

	if volumeID != "" {
		payload.Path, err = filesystem.BuildPathToFileInsideVolume(volumeID, payload.Path)
		if err != nil {
			return &httperror.HandlerError{http.StatusBadRequest, "Invalid volume", err}
		}

		_, err = filesystem.BuildPathToFileInsideVolume(volumeID, payload.Filename)
		if err != nil {
			return &httperror.HandlerError{http.StatusBadRequest, "Invalid filename", err}
		}
	}

	// Handle chunks
	// Step 1: Start to recieve a new file
	if handler.recvFile.path != payload.Path || handler.recvFile.name != payload.Filename {
		handler.recvFile.path = payload.Path
		handler.recvFile.name = payload.Filename
		handler.recvFile.chunkSize, err = strconv.ParseInt(r.FormValue("_chunkSize"), 10, 64)
		if err != nil {
			return &httperror.HandlerError{http.StatusBadRequest, "Invalid request payload: missing chunk size", err}
		}
		err := os.MkdirAll(payload.Path, 0755)
		if err != nil {
			return &httperror.HandlerError{http.StatusBadRequest, "Failed to create dir", err}
		}
		if handler.recvFile.dstFile != nil {
			// Close last file if tranfer is interupted.
			handler.recvFile.dstFile.Close()
		}
		handler.recvFile.dstFile, err = os.OpenFile(path.Join(payload.Path, payload.Filename), os.O_CREATE|os.O_WRONLY, os.ModePerm)
		if err != nil {
			return &httperror.HandlerError{http.StatusBadRequest, "Failed to create file", err}
		}
		handler.recvFile.dstWriter = bufio.NewWriter(handler.recvFile.dstFile)
	}
	// Step 2: Write chunk into the list
	var currentChunkSize int64
	currentChunkSize, err = strconv.ParseInt(r.FormValue("_currentChunkSize"), 10, 64)
	if err != nil {
		return &httperror.HandlerError{http.StatusBadRequest, "Invalid request payload: missing current chunk size", err}
	}
	if currentChunkSize > 0 {
		srcfile, err := payload.Header.Open()
		if err != nil {
			return &httperror.HandlerError{http.StatusBadRequest, "Invalid request payload", err}
		}
		defer srcfile.Close()
		var buffer []byte
		buffer, err = io.ReadAll(srcfile)
		if err != nil {
			return &httperror.HandlerError{http.StatusBadRequest, "Invalid request payload", err}
		}
		_, err = handler.recvFile.dstWriter.Write(buffer)
		if err != nil && err != io.EOF {
			return &httperror.HandlerError{http.StatusBadRequest, "Failed to save chunk", err}
		}
	}
	// Step 3: Close file and finish job if recived last chunk
	if currentChunkSize != handler.recvFile.chunkSize {
		handler.recvFile.dstWriter.Flush()
		handler.recvFile.dstFile.Close()
		// Clean up before err check
		handler.recvFile.path = ""
		handler.recvFile.name = ""
		handler.recvFile.dstFile = nil
	}

	// For debug
	chunknum, err := strconv.ParseInt(r.FormValue("_chunkNumber"), 10, 64)
	if err != nil {
		return &httperror.HandlerError{http.StatusBadRequest, "Invalid request _chunkNumber", err}
	}
	log.Println("[INFO] chunk number: ", chunknum)

	return response.Empty(rw)
}

// POST request on /v1/browse/:id/put
func (handler *Handler) browsePutV1(rw http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	volumeID, err := request.RetrieveRouteVariableValue(r, "id")
	if err != nil {
		return &httperror.HandlerError{http.StatusBadRequest, "Invalid volume identifier route variable", err}
	}

	var payload browsePutPayload
	err = payload.Validate(r)
	if err != nil {
		return &httperror.HandlerError{http.StatusBadRequest, "Invalid request payload", err}
	}
	payload.Path, err = filesystem.BuildPathToFileInsideVolume(volumeID, payload.Path)
	if err != nil {
		return &httperror.HandlerError{http.StatusBadRequest, "Invalid volume", err}
	}

	err = filesystem.WriteFile(payload.Path, payload.Filename, payload.File, 0755)
	if err != nil {
		return &httperror.HandlerError{http.StatusInternalServerError, "Error saving file to disk", err}
	}

	return response.Empty(rw)
}
