package docker

import "context"

func VolumeDelete(name string, force bool) error {
	cli, err := getCli()
	if err != nil {
		return err
	}

	return cli.VolumeRemove(context.Background(), name, force)
}