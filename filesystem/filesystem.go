package filesystem

import (
	"bufio"
	"errors"
	"io"
	"io/ioutil"
	"mime/multipart"
	"os"
	"path"
	"strings"
	"time"

	"github.com/portainer/agent/constants"
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

// ReadFromFile returns the content of a file.
func ReadFromFile(filePath string) ([]byte, error) {
	return ioutil.ReadFile(filePath)
}

// FileExists will verify that a file exists under the specified file path.
func FileExists(filePath string) (bool, error) {
	if _, err := os.Stat(filePath); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
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
func WriteFile(folder, filename string, file []byte, mode uint32) error {
	err := os.MkdirAll(folder, 0755)
	if err != nil {
		return err
	}

	filePath := path.Join(folder, filename)

	return ioutil.WriteFile(filePath, file, os.FileMode(mode))
}

// WriteFile takes a path, filename, a file and the mode that should be associated
// to the file and writes it to disk
func WriteBigFile(folder, filename string, fileheader *multipart.FileHeader, mode uint32) error {
	srcfile, err := fileheader.Open()
	if err != nil {
		return err
	}
	defer srcfile.Close()

	err = os.MkdirAll(folder, 0755)
	if err != nil {
		return err
	}
	filePath := path.Join(folder, filename)

	dstfile, err2 := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY, os.ModePerm)
	if err2 != nil {
		return err2
	}
	defer dstfile.Close()

	const chunkSize int64 = 32 << 20 // 32 MB
	buf := make([]byte, chunkSize)

	reader := bufio.NewReader(srcfile)
	writer := bufio.NewWriter(dstfile)

	var n int
	for {
		n, err = reader.Read(buf)
		if err != nil && err != io.EOF {
			return err
		}
		if int64(n) < chunkSize {
			if n > 0 {
				_, err = writer.Write(buf[:n])
				if err != nil && err != io.EOF {
					return err
				}
			}
			break
		} else {
			_, err = writer.Write(buf)
			if err != nil && err != io.EOF {
				return err
			}
		}
	}
	writer.Flush()
	return nil
}

// BuildPathToFileInsideVolume will take a volumeID and path, and build a full path on the host
func BuildPathToFileInsideVolume(volumeID, filePath string) (string, error) {
	if !isValidPath(filePath) {
		return "", errors.New("Invalid path. Ensure that the path do not contain '..' elements")
	}

	return path.Join(constants.SystemVolumePath, volumeID, "_data", filePath), nil
}

func isValidPath(path string) bool {
	return !containsDotDot(path)
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
