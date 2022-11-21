package os

import (
	"os"
	"path"
)

func GetHostName() (string, error) {
	return os.Hostname()
}

func DeleteDockerConfig() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	dockerConfigPath := path.Join(home, ".docker", "config.json")

	return os.Remove(dockerConfigPath)
}
