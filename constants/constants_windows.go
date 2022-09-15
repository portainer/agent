//go:build windows
// +build windows

package constants

const (
	// SystemVolumePath represents the path where the volumes are stored on the filesystem
	SystemVolumePath = `C:\ProgramData\Docker\volumes`
	DockerSocketBind = `\\.\pipe\docker_engine:\\.\pipe\docker_engine`
)
