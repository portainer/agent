package stack

// IsRelativePathStack checks if the edge stack enables relative path or not
func IsRelativePathStack(stack *edgeStack) bool {
	return stack.SupportRelativePath && stack.FilesystemPath != ""
}
