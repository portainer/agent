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

// OpenFileInsideVolume will open a file inside a volume and return a FileDetails pointer
// with information about this file.
// The returned FileDetails contains a pointer to the File that must be closed manually
func OpenFileInsideVolume(volumeID, filePath string) (*FileDetails, error) {
	pathInsideVolume, err := buildPathToFileInsideVolume(volumeID, filePath)
	if err != nil {
		return nil, err
	}

	file, err := os.Open(pathInsideVolume)
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
		BasePath: path.Base(pathInsideVolume),
	}

	return fileDetails, nil
}

// RemoveFileInsideVolume will remove a file inside a volume
func RemoveFileInsideVolume(volumeID, filePath string) error {
	pathInsideVolume, err := buildPathToFileInsideVolume(volumeID, filePath)
	if err != nil {
		return err
	}

	return os.Remove(pathInsideVolume)
}

// ListFilesInsideVolumeDirectory returns a slice of FileInfo for each file in the specified directory inside a volume
func ListFilesInsideVolumeDirectory(volumeID, directoryPath string) ([]FileInfo, error) {
	pathInsideVolume, err := buildPathToFileInsideVolume(volumeID, directoryPath)
	if err != nil {
		return nil, err
	}

	files, err := ioutil.ReadDir(pathInsideVolume)
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

// RenameFileInsideVolume will rename a file inside a volume
func RenameFileInsideVolume(volumeID, oldPath, newPath string) error {
	oldPathInsideVolume, err := buildPathToFileInsideVolume(volumeID, oldPath)
	if err != nil {
		return err
	}

	newPathInsideVolume, err := buildPathToFileInsideVolume(volumeID, newPath)
	if err != nil {
		return err
	}

	return os.Rename(oldPathInsideVolume, newPathInsideVolume)
}

// UploadFileToVolume takes a file, volume, and path, and writes it to that volume
func UploadFileToVolume(volumeID, path, filename string, file []byte) error {

	pathInsideVolume, err := buildPathToFileInsideVolume(volumeID, path)
	if err != nil {
		return err
	}

	err = ioutil.WriteFile(pathInsideVolume+filename, file, 0644)
	if err != nil {
		return err
	}

	return err
}

func buildPathToFileInsideVolume(volumeID, filePath string) (string, error) {
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
