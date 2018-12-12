package filesystem

import (
	"io/ioutil"
	"os"
	"path"
	"strings"
	"time"

	"github.com/portainer/agent"
	"github.com/portainer/agent/constants"
)

const (
	errInvalidFilePath = agent.Error("Invalid path. Ensure that the path do not contain '..' elements")
)

// FileInfo represents information about a file on the filesystem
type FileInfo struct {
	Name    string `json:"Name"`
	Size    int64  `json:"Size"`
	Dir     bool   `json:"Dir"`
	ModTime int64  `json:"ModTime"`
}

// FileDetails is a wrapper around a *os.File and contains extra information on the file
type FileDetails struct {
	File     *os.File
	ModTime  time.Time
	BasePath string
}

// OpenFile will open a file and return a FileDetails pointer
// with information about this file.
// The returned FileDetails contains a pointer to the File that must be closed manually
func OpenFile(filePath string) (*FileDetails, error) {

	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}

	fileInfo, err := file.Stat()
	if err != nil {
		return nil, err
	}

	fileDetails := &FileDetails{
		File:     file,
		ModTime:  fileInfo.ModTime(),
		BasePath: path.Base(filePath),
	}

	return fileDetails, nil
}

// RemoveFile will remove a file
func RemoveFile(filePath string) error {
	return os.Remove(filePath)
}

// ListFilesInsideDirectory returns a slice of FileInfo for each file in the specified directory inside a volume
func ListFilesInsideDirectory(directoryPath string) ([]FileInfo, error) {

	files, err := ioutil.ReadDir(directoryPath)
	if err != nil {
		return nil, err
	}

	fileList := make([]FileInfo, 0)
	for _, f := range files {
		file := FileInfo{
			Name:    f.Name(),
			Size:    f.Size(),
			Dir:     f.IsDir(),
			ModTime: f.ModTime().Unix(),
		}

		fileList = append(fileList, file)
	}

	return fileList, nil
}

// RenameFile will rename a file
func RenameFile(oldPath, newPath string) error {
	return os.Rename(oldPath, newPath)
}

// WriteFile takes a path, filename, a file and the mode that should be associated
// to the file and writes it to disk
func WriteFile(uploadedFilePath, filename string, file []byte, mode uint32) error {
	os.MkdirAll(uploadedFilePath, 0755)
	filePath := path.Join(uploadedFilePath, filename)

	err := ioutil.WriteFile(filePath, file, os.FileMode(mode))
	if err != nil {
		return err
	}

	return nil
}

// BuildPathToFileInsideVolume will take a volumeID and path, and build a full path on the host
func BuildPathToFileInsideVolume(volumeID, filePath string) (string, error) {
	if !isValidPath(filePath) {
		return "", errInvalidFilePath
	}

	return path.Join(constants.SystemVolumePath, volumeID, "_data", filePath), nil
}

func isValidPath(path string) bool {
	if containsDotDot(path) {
		return false
	}
	return true
}

func containsDotDot(v string) bool {
	if !strings.Contains(v, "..") {
		return false
	}
	for _, ent := range strings.FieldsFunc(v, isSlashRune) {
		if ent == ".." {
			return true
		}
	}
	return false
}

func isSlashRune(r rune) bool { return r == '/' || r == '\\' }
