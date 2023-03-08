package updates

import (
	"context"

	nomadapi "github.com/hashicorp/nomad/api"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
)

type NomadUpdaterCleaner struct {
	updateID int
}

func NewNomadUpdaterCleaner(updateID int) *NomadUpdaterCleaner {
	return &NomadUpdaterCleaner{
		updateID: updateID,
	}
}

func (nu *NomadUpdaterCleaner) Clean(ctx context.Context) error {
	// Create a Nomad API client configuration
	client, err := nomadapi.NewClient(nomadapi.DefaultConfig())
	if err != nil {
		return errors.Wrap(err, "failed to init Nomad api client")
	}

	// Remove the job
	jobID := "portainer-updater"

	job, _, err := client.Jobs().Info(jobID, nil)
	if err != nil {
		log.Info().Err(err).Msg("failed to find nomad job  portainer-update")
		return nil
	}

	_, _, err = client.Jobs().Deregister(*job.ID, true, &nomadapi.WriteOptions{
		Region:    *job.Region,
		Namespace: *job.Namespace,
	})
	if err != nil {
		return errors.Wrap(err, "failed to remove nomad job portainer-updater")
	}

	// Confirm the job was removed. Expect an error as the
	// job should not exist
	_, _, err = client.Jobs().Info(*job.ID, nil)
	if err == nil {
		return errors.Wrap(err, "nomad job portainer-updater still exists")
	}

	return nil
}

func (nu *NomadUpdaterCleaner) UpdateID() int {
	return nu.updateID
}
