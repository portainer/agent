package stack

import (
	"fmt"
	"strconv"

	"github.com/portainer/agent"
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

func getStackFileFolder(stack *edgeStack) string {
	stackIDStr := strconv.Itoa(stack.ID)

	if IsHelmStack(stack) {
		return filesystem.JoinPaths(agent.EdgeStackFilesPath, stackIDStr)
	}

	folder := filesystem.JoinPaths(agent.EdgeStackFilesPath, stackIDStr)
	if stack.EdgeUpdateID != 0 {
		folder = filesystem.JoinPaths(agent.UpdateEdgeStackFilesPath, stackIDStr)
	} else if IsRelativePathStack(stack) {
		folder = filesystem.JoinPaths(stack.FilesystemPath, agent.ComposePathPrefix, stackIDStr)
	}

	return folder
}

// IsHelmStack reports whether the stack is any kind of helm stack
// (either helm repository or git repository helm).
func IsHelmStack(stack *edgeStack) bool {
	return IsHelmRepoStack(stack) || IsGitRepoHelmStack(stack)
}

// IsHelmRepoStack reports whether the stack is a helm repository stack
func IsHelmRepoStack(stack *edgeStack) bool {
	return stack.HelmConfig.ChartURL != ""
}

// IsGitRepoHelmStack reports whether the stack is a git repository helm stack
func IsGitRepoHelmStack(stack *edgeStack) bool {
	return stack.HelmConfig.ChartPath != ""
}
