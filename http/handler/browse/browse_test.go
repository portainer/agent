package browse

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gorilla/mux"
	"github.com/portainer/agent/filesystem"
	cefilesystem "github.com/portainer/portainer/api/filesystem"
	httperror "github.com/portainer/portainer/pkg/libhttp/error"
	"github.com/stretchr/testify/require"
)

// newTestHandler stubs resolveVolumePathFunc for the duration of the test and
// restores the original via t.Cleanup.
//
// Stub behaviour:
//   - volumeID "unmounted" -> ErrSystemVolumePathNotMounted
//   - volumeID "invalid"   -> a generic error (triggers 400)
//   - any other volumeID   -> resolvedPath, nil
func newTestHandler(t *testing.T, resolvedPath string) *Handler {
	t.Helper()
	orig := resolveVolumePathFunc
	resolveVolumePathFunc = func(volumeID, _ string) (string, error) {
		switch volumeID {
		case "unmounted":
			return "", filesystem.ErrSystemVolumePathNotMounted
		case "invalid":
			return "", errInvalidVolume
		default:
			return resolvedPath, nil
		}
	}
	t.Cleanup(func() { resolveVolumePathFunc = orig })
	return &Handler{Router: mux.NewRouter()}
}

var errInvalidVolume = &browseError{"invalid volume"}

type browseError struct{ msg string }

func (e *browseError) Error() string { return e.msg }

// serve wraps a LoggerHandler so that the returned *HandlerError is written
// as an HTTP error response, mirroring what the real mux wiring does.
func serve(h httperror.LoggerHandler, req *http.Request) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

// ---------- browseList ----------

func TestBrowseList_NoVolumeID_ListsDirectory(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(cefilesystem.JoinPaths(dir, "a.txt"), []byte("x"), 0o600))

	h := newTestHandler(t, dir)
	req := httptest.NewRequest(http.MethodGet, "/browse/ls?path="+dir, nil)
	rr := serve(h.browseList, req)

	require.Equal(t, http.StatusOK, rr.Code)

	var files []filesystem.FileInfo
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&files))
	require.Len(t, files, 1)
	require.Equal(t, "a.txt", files[0].Name)
}

func TestBrowseList_WithVolumeID_ListsResolvedPath(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(cefilesystem.JoinPaths(dir, "file.txt"), []byte("y"), 0o600))

	h := newTestHandler(t, dir)
	req := httptest.NewRequest(http.MethodGet, "/browse/ls?volumeID=my_vol&path=file.txt", nil)
	rr := serve(h.browseList, req)

	require.Equal(t, http.StatusOK, rr.Code)
}

func TestBrowseList_VolumeNotMounted_Returns500(t *testing.T) {
	h := newTestHandler(t, "")
	req := httptest.NewRequest(http.MethodGet, "/browse/ls?volumeID=unmounted&path=file.txt", nil)
	rr := serve(h.browseList, req)

	require.Equal(t, http.StatusInternalServerError, rr.Code)
}

func TestBrowseList_InvalidVolume_Returns400(t *testing.T) {
	h := newTestHandler(t, "")
	req := httptest.NewRequest(http.MethodGet, "/browse/ls?volumeID=invalid&path=file.txt", nil)
	rr := serve(h.browseList, req)

	require.Equal(t, http.StatusBadRequest, rr.Code)
}

// ---------- browseListV1 ----------

func TestBrowseListV1_ListsResolvedPath(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(cefilesystem.JoinPaths(dir, "v1.txt"), []byte("v"), 0o600))

	h := newTestHandler(t, dir)
	rr := serveV1Route("/v1/browse/{id}/ls", "/v1/browse/my_vol/ls?path=v1.txt", http.MethodGet, nil, h.browseListV1)

	require.Equal(t, http.StatusOK, rr.Code)
}

func TestBrowseListV1_VolumeNotMounted_Returns500(t *testing.T) {
	h := newTestHandler(t, "")
	rr := serveV1Route("/v1/browse/{id}/ls", "/v1/browse/unmounted/ls?path=file.txt", http.MethodGet, nil, h.browseListV1)

	require.Equal(t, http.StatusInternalServerError, rr.Code)
}

// ---------- browseGet ----------

func TestBrowseGet_NoVolumeID_ServesFile(t *testing.T) {
	dir := t.TempDir()
	f := cefilesystem.JoinPaths(dir, "data.txt")
	require.NoError(t, os.WriteFile(f, []byte("content"), 0o600))

	h := newTestHandler(t, dir)
	req := httptest.NewRequest(http.MethodGet, "/browse/get?path="+f, nil)
	rr := serve(h.browseGet, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.Equal(t, "content", rr.Body.String())
}

func TestBrowseGet_WithVolumeID_ServesResolvedFile(t *testing.T) {
	dir := t.TempDir()
	f := cefilesystem.JoinPaths(dir, "vol.txt")
	require.NoError(t, os.WriteFile(f, []byte("voldata"), 0o600))

	h := newTestHandler(t, f)
	req := httptest.NewRequest(http.MethodGet, "/browse/get?volumeID=my_vol&path=vol.txt", nil)
	rr := serve(h.browseGet, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.Equal(t, "voldata", rr.Body.String())
}

func TestBrowseGet_VolumeNotMounted_Returns500(t *testing.T) {
	h := newTestHandler(t, "")
	req := httptest.NewRequest(http.MethodGet, "/browse/get?volumeID=unmounted&path=x.txt", nil)
	rr := serve(h.browseGet, req)

	require.Equal(t, http.StatusInternalServerError, rr.Code)
}

// ---------- browseGetV1 ----------

func TestBrowseGetV1_ServesResolvedFile(t *testing.T) {
	dir := t.TempDir()
	f := cefilesystem.JoinPaths(dir, "v1data.txt")
	require.NoError(t, os.WriteFile(f, []byte("v1content"), 0o600))

	h := newTestHandler(t, f)
	rr := serveV1Route("/v1/browse/{id}/get", "/v1/browse/my_vol/get?path=v1data.txt", http.MethodGet, nil, h.browseGetV1)

	require.Equal(t, http.StatusOK, rr.Code)
	require.Equal(t, "v1content", rr.Body.String())
}

func TestBrowseGetV1_VolumeNotMounted_Returns500(t *testing.T) {
	h := newTestHandler(t, "")
	rr := serveV1Route("/v1/browse/{id}/get", "/v1/browse/unmounted/get?path=x.txt", http.MethodGet, nil, h.browseGetV1)

	require.Equal(t, http.StatusInternalServerError, rr.Code)
}

// ---------- browseDelete ----------

func TestBrowseDelete_NoVolumeID_DeletesFile(t *testing.T) {
	dir := t.TempDir()
	f := cefilesystem.JoinPaths(dir, "del.txt")
	require.NoError(t, os.WriteFile(f, []byte("x"), 0o600))

	h := newTestHandler(t, dir)
	req := httptest.NewRequest(http.MethodDelete, "/browse/delete?path="+f, nil)
	rr := serve(h.browseDelete, req)

	require.Equal(t, http.StatusNoContent, rr.Code)
	_, err := os.Stat(f)
	require.True(t, os.IsNotExist(err))
}

func TestBrowseDelete_WithVolumeID_DeletesResolvedFile(t *testing.T) {
	dir := t.TempDir()
	f := cefilesystem.JoinPaths(dir, "del2.txt")
	require.NoError(t, os.WriteFile(f, []byte("x"), 0o600))

	h := newTestHandler(t, f)
	req := httptest.NewRequest(http.MethodDelete, "/browse/delete?volumeID=my_vol&path=del2.txt", nil)
	rr := serve(h.browseDelete, req)

	require.Equal(t, http.StatusNoContent, rr.Code)
	_, err := os.Stat(f)
	require.True(t, os.IsNotExist(err))
}

func TestBrowseDelete_VolumeNotMounted_Returns500(t *testing.T) {
	h := newTestHandler(t, "")
	req := httptest.NewRequest(http.MethodDelete, "/browse/delete?volumeID=unmounted&path=x.txt", nil)
	rr := serve(h.browseDelete, req)

	require.Equal(t, http.StatusInternalServerError, rr.Code)
}

// ---------- browseDeleteV1 ----------

func TestBrowseDeleteV1_DeletesResolvedFile(t *testing.T) {
	dir := t.TempDir()
	f := cefilesystem.JoinPaths(dir, "v1del.txt")
	require.NoError(t, os.WriteFile(f, []byte("x"), 0o600))

	h := newTestHandler(t, f)
	rr := serveV1Route("/v1/browse/{id}/delete", "/v1/browse/my_vol/delete?path=v1del.txt", http.MethodDelete, nil, h.browseDeleteV1)

	require.Equal(t, http.StatusNoContent, rr.Code)
	_, err := os.Stat(f)
	require.True(t, os.IsNotExist(err))
}

func TestBrowseDeleteV1_VolumeNotMounted_Returns500(t *testing.T) {
	h := newTestHandler(t, "")
	rr := serveV1Route("/v1/browse/{id}/delete", "/v1/browse/unmounted/delete?path=x.txt", http.MethodDelete, nil, h.browseDeleteV1)

	require.Equal(t, http.StatusInternalServerError, rr.Code)
}

// ---------- browseRename ----------

func TestBrowseRename_NoVolumeID_RenamesFile(t *testing.T) {
	dir := t.TempDir()
	src := cefilesystem.JoinPaths(dir, "old.txt")
	dst := cefilesystem.JoinPaths(dir, "new.txt")
	require.NoError(t, os.WriteFile(src, []byte("x"), 0o600))

	h := newTestHandler(t, dir)

	body, _ := json.Marshal(map[string]string{
		"CurrentFilePath": src,
		"NewFilePath":     dst,
	})
	req := httptest.NewRequest(http.MethodPut, "/browse/rename", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := serve(h.browseRename, req)

	require.Equal(t, http.StatusNoContent, rr.Code)
	_, err := os.Stat(dst)
	require.NoError(t, err)
}

func TestBrowseRename_VolumeNotMounted_Returns500(t *testing.T) {
	h := newTestHandler(t, "")

	body, _ := json.Marshal(map[string]string{
		"CurrentFilePath": "old.txt",
		"NewFilePath":     "new.txt",
	})
	req := httptest.NewRequest(http.MethodPut, "/browse/rename?volumeID=unmounted", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := serve(h.browseRename, req)

	require.Equal(t, http.StatusInternalServerError, rr.Code)
}

// ---------- browseRenameV1 ----------

func TestBrowseRenameV1_RenamesFile(t *testing.T) {
	dir := t.TempDir()
	src := cefilesystem.JoinPaths(dir, "v1old.txt")
	dst := cefilesystem.JoinPaths(dir, "v1new.txt")
	require.NoError(t, os.WriteFile(src, []byte("x"), 0o600))

	// Two successive calls: first for CurrentFilePath, then for NewFilePath.
	call := 0
	paths := []string{src, dst}
	orig := resolveVolumePathFunc
	resolveVolumePathFunc = func(_, _ string) (string, error) {
		p := paths[call%2]
		call++
		return p, nil
	}
	t.Cleanup(func() { resolveVolumePathFunc = orig })

	h := &Handler{Router: mux.NewRouter()}

	body, _ := json.Marshal(map[string]string{
		"CurrentFilePath": "v1old.txt",
		"NewFilePath":     "v1new.txt",
	})
	rr := serveV1Route("/v1/browse/{id}/rename", "/v1/browse/my_vol/rename", http.MethodPut,
		bytes.NewReader(body), h.browseRenameV1, withContentType("application/json"))

	require.Equal(t, http.StatusNoContent, rr.Code)
	_, err := os.Stat(dst)
	require.NoError(t, err)
	_, err = os.Stat(src)
	require.True(t, os.IsNotExist(err))
}

func TestBrowseRenameV1_VolumeNotMounted_Returns500(t *testing.T) {
	h := newTestHandler(t, "")

	body, _ := json.Marshal(map[string]string{
		"CurrentFilePath": "old.txt",
		"NewFilePath":     "new.txt",
	})
	rr := serveV1Route("/v1/browse/{id}/rename", "/v1/browse/unmounted/rename", http.MethodPut,
		bytes.NewReader(body), h.browseRenameV1, withContentType("application/json"))

	require.Equal(t, http.StatusInternalServerError, rr.Code)
}

// ---------- browsePutV1 ----------

func TestBrowsePutV1_WritesFile(t *testing.T) {
	dir := t.TempDir()
	h := newTestHandler(t, dir)

	body, ct := multipartForm(t, "somepath", "upload.txt", []byte("hello"))
	rr := serveV1Route("/v1/browse/{id}/put", "/v1/browse/my_vol/put", http.MethodPost,
		body, h.browsePutV1, withContentType(ct))

	require.Equal(t, http.StatusNoContent, rr.Code)

	content, err := os.ReadFile(cefilesystem.JoinPaths(dir, "upload.txt"))
	require.NoError(t, err)
	require.Equal(t, "hello", string(content))
}

func TestBrowsePutV1_VolumeNotMounted_Returns500(t *testing.T) {
	h := newTestHandler(t, "")

	body, ct := multipartForm(t, "somepath", "upload.txt", []byte("hello"))
	rr := serveV1Route("/v1/browse/{id}/put", "/v1/browse/unmounted/put", http.MethodPost,
		body, h.browsePutV1, withContentType(ct))

	require.Equal(t, http.StatusInternalServerError, rr.Code)
}

// ---------- helpers ----------

type reqOption func(*http.Request)

func withContentType(ct string) reqOption {
	return func(r *http.Request) { r.Header.Set("Content-Type", ct) }
}

// serveV1Route mounts the handler on a temporary mux so gorilla/mux populates
// route variables (e.g. {id}), then serves the given target URL.
func serveV1Route(pattern, target, method string, body interface{ Read([]byte) (int, error) }, handler httperror.LoggerHandler, opts ...reqOption) *httptest.ResponseRecorder {
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, target, body)
	} else {
		req = httptest.NewRequest(method, target, nil)
	}
	for _, o := range opts {
		o(req)
	}

	router := mux.NewRouter()
	router.Handle(pattern, handler)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	return rr
}

// multipartForm builds a multipart body with a Path field and a file field.
// Returns the body reader and the Content-Type header value.
func multipartForm(t *testing.T, path, filename string, content []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	require.NoError(t, mw.WriteField("Path", path))
	fw, err := mw.CreateFormFile("file", filename)
	require.NoError(t, err)
	_, err = fw.Write(content)
	require.NoError(t, err)
	require.NoError(t, mw.Close())
	return &buf, mw.FormDataContentType()
}
