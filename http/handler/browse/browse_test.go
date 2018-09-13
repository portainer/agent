package browse

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path"
	"strings"
	"testing"

	"github.com/portainer/agent"
)

func TestBrowseSuccesses(t *testing.T) {
	var tags map[string]string
	var clusterService agent.ClusterService
	handler := NewHandler(clusterService, tags)
	filepath := "/testing"
	file := "put_test.txt"

	t.Run("BrowsePut", func(t *testing.T) {
		values := map[string]io.Reader{
			"file": mustOpen(file),
			"Path": strings.NewReader(filepath),
		}

		var b bytes.Buffer
		w := multipart.NewWriter(&b)
		for key, r := range values {
			var fw io.Writer
			var err error
			if x, ok := r.(io.Closer); ok {
				defer x.Close()
			}
			if x, ok := r.(*os.File); ok {
				if fw, err = w.CreateFormFile(key, x.Name()); err != nil {
					return
				}
			} else {
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
	})

	t.Run("BrowseDelete", func(t *testing.T) {
		deletePath := path.Join(filepath, file)
		request := httptest.NewRequest("DELETE", "/browse/test/delete?path="+deletePath, nil)
		writer := httptest.NewRecorder()
		handler.ServeHTTP(writer, request)
		if writer.Result().StatusCode != 204 {
			t.Error("Failed to delete file.", writer.Result().Status)
		}
	})
}

func TestBrowseFail(t *testing.T) {
	var tags map[string]string
	var clusterService agent.ClusterService
	handler := NewHandler(clusterService, tags)
	file := "put_test.txt"

	t.Run("BrowsePutMissingPath", func(t *testing.T) {
		values := map[string]io.Reader{
			"file": mustOpen(file),
		}

		var b bytes.Buffer
		w := multipart.NewWriter(&b)
		for key, r := range values {
			var fw io.Writer
			var err error
			if x, ok := r.(io.Closer); ok {
				defer x.Close()
			}
			if x, ok := r.(*os.File); ok {
				if fw, err = w.CreateFormFile(key, x.Name()); err != nil {
					return
				}
			} else {
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
		if writer.Result().StatusCode != 400 {
			t.Error("Failed to handle missing path", writer.Result().Status)
		}
	})

	t.Run("BrowsePutMissingFile", func(t *testing.T) {
		filepath := "/testing"
		values := map[string]io.Reader{
			"Path": strings.NewReader(filepath),
		}
		var b bytes.Buffer
		w := multipart.NewWriter(&b)
		for key, r := range values {
			var fw io.Writer
			var err error
			if x, ok := r.(io.Closer); ok {
				defer x.Close()
			}
			if x, ok := r.(*os.File); ok {
				if fw, err = w.CreateFormFile(key, x.Name()); err != nil {
					return
				}
			} else {
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
		if writer.Result().StatusCode != 400 {
			t.Error("Failed to handle missing file", writer.Result().Status)
		}
	})

	t.Run("BrowsePutUnableToStoreFile", func(t *testing.T) {
		filepath := "/testing"
		values := map[string]io.Reader{
			"file": mustOpen(file),
			"Path": strings.NewReader(filepath),
		}

		var b bytes.Buffer
		w := multipart.NewWriter(&b)
		for key, r := range values {
			var fw io.Writer
			var err error
			if x, ok := r.(io.Closer); ok {
				defer x.Close()
			}
			if x, ok := r.(*os.File); ok {
				if fw, err = w.CreateFormFile(key, x.Name()); err != nil {
					return
				}
			} else {
				if fw, err = w.CreateFormField(key); err != nil {
					return
				}
			}
			if _, err = io.Copy(fw, r); err != nil {
				return
			}

		}
		w.Close()

		request := httptest.NewRequest("POST", "/browse/bad/put", &b)
		request.Header.Set("Content-Type", w.FormDataContentType())
		writer := httptest.NewRecorder()
		handler.ServeHTTP(writer, request)
		if writer.Result().StatusCode != 500 {
			t.Error("Failed", writer.Result().Status)
		}
	})

	t.Run("BrowseDeleteMissingPath", func(t *testing.T) {
		request := httptest.NewRequest("DELETE", "/browse/test/delete", nil)
		writer := httptest.NewRecorder()
		handler.ServeHTTP(writer, request)
		if writer.Result().StatusCode != 400 {
			t.Error("Didn't return an error for missing path", writer.Result().Status)
		}
	})
}

func mustOpen(f string) *os.File {
	r, err := os.Open(f)
	if err != nil {
		panic(err)
	}
	return r
}
