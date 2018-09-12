package browse

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/portainer/agent"
)

func TestBrowsePut(t *testing.T) {

	var tags map[string]string
	var clusterService agent.ClusterService
	handler := NewHandler(clusterService, tags)
	values := map[string]io.Reader{
		"file": mustOpen("put_test.txt"),
		"Path": strings.NewReader("/testing"),
	}

	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	for key, r := range values {
		var fw io.Writer
		var err error
		if x, ok := r.(io.Closer); ok {
			defer x.Close()
		}
		// Add an image file
		if x, ok := r.(*os.File); ok {
			if fw, err = w.CreateFormFile(key, x.Name()); err != nil {
				return
			}
		} else {
			// Add other fields
			if fw, err = w.CreateFormField(key); err != nil {
				return
			}
		}
		if _, err = io.Copy(fw, r); err != nil {
			return
		}

	}
	w.Close()

	request := httptest.NewRequest("POST", "/browse/test/put", &b)
	request.Header.Set("Content-Type", w.FormDataContentType())
	writer := httptest.NewRecorder()
	handler.ServeHTTP(writer, request)
	if writer.Result().StatusCode != 204 {
		t.Error("Failed to upload file.", writer.Result().Status)
	}
}

func mustOpen(f string) *os.File {
	r, err := os.Open(f)
	if err != nil {
		panic(err)
	}
	return r
}
