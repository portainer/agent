package stack

import (
	"fmt"

	"github.com/portainer/portainer/api/filesystem"
)

// successFolderSuffix is suffix for the path where the last successfully deployed edge stack files are saved
const successFolderSuffix = ".success"

// IsRelativePathStack checks if the edge stack enables relative path or not
func IsRelativePathStack(stack *edgeStack) bool {
	return stack.SupportRelativePath && stack.FilesystemPath != ""
}

func SuccessStackFileFolder(fileFolder string) string {
	return fmt.Sprintf("%s%s", fileFolder, successFolderSuffix)
}

func backupSuccessStack(stack *edgeStack) error {
	src := stack.FileFolder
	dst := SuccessStackFileFolder(src)

	return filesystem.CopyDir(src, dst, false)
}
