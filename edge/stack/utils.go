package stack

import (
	"fmt"
	"github.com/portainer/agent"
	"github.com/portainer/portainer/api/filesystem"
)

// IsRelativePathStack checks if the edge stack enables relative path or not
func IsRelativePathStack(stack *edgeStack) bool {
	return stack.SupportRelativePath && stack.FilesystemPath != ""
}

func SuccessStackFileFolder(fileFolder string) string {
	return fmt.Sprintf("%s%s", fileFolder, agent.EdgeStackSuccessFilesFolderSuffix)
}

func backupSuccessStack(stack *edgeStack) error {
	src := stack.FileFolder
	dst := SuccessStackFileFolder(src)
	return filesystem.CopyDir(src, dst, false)
}
