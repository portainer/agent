package nomad

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	nomadapi "github.com/hashicorp/nomad/api"
	"github.com/pkg/errors"
	"github.com/portainer/agent/filesystem"
)

// Deployer represents a service to deploy resources inside a Nomad environment.
type Deployer struct {
	client *nomadapi.Client
}

// NewDeployer initializes a new Nomad api client.
func NewDeployer() (*Deployer, error) {
	//DefaultConfig will try to retrieve NOMAD_ADDR and NOMAD_TOKEN from ENV
	client, err := nomadapi.NewClient(nomadapi.DefaultConfig())
	if err != nil {
		return nil, errors.Wrap(err, "failed to init Nomad api client")
	}
	return &Deployer{client: client}, nil
}

// Deploy attempts to run a Nomad job via provided job file
func (d *Deployer) Deploy(ctx context.Context, name string, filePaths []string, prune bool) error {
	if len(filePaths) == 0 {
		return errors.New("missing Nomad job file paths")
	}

	bakFileName := fmt.Sprintf("%s_bak.hcl", name)
	bakFileFolder := filepath.Dir(filePaths[0])
	backFilePath := filepath.Join(bakFileFolder, bakFileName)

	newJobFile, err := filesystem.ReadFromFile(filePaths[0])
	if err != nil {
		return errors.Wrap(err, "failed to read Nomad job file")
	}

	newJob, err := d.client.Jobs().ParseHCL(string(newJobFile), true)
	if err != nil {
		return errors.Wrap(err, "failed to parse Nomad job file")
	}

	// An existing backup file means it is an update action
	// Need to check if the new coming job file has different region, namespace or id settings
	// If yes, delete the former job
	if backupFileExists, _ := filesystem.FileExists(backFilePath); backupFileExists {
		oldJobFile, err := filesystem.ReadFromFile(backFilePath)
		if err != nil {
			return errors.Wrap(err, "failed to read Nomad job file")
		}
		oldJob, err := d.client.Jobs().ParseHCL(string(oldJobFile), true)
		if err != nil {
			return errors.Wrap(err, "failed to parse backup Nomad job file")
		}

		// If new job has critical config changes
		// Purge the old job before register the new one
		if diff := compareJobs(newJob, oldJob); diff {
			err = d.verifyAndPurgeJob(oldJob)
			if err != nil {
				return errors.Wrap(err, "failed to purge former Nomad job")
			}
		}
		filesystem.RemoveFile(backFilePath)
	}
	// Check if the job is periodic or is a parameterized job
	periodic := newJob.IsPeriodic()
	paramjob := newJob.IsParameterized()
	multiregion := newJob.IsMultiregion()

	// Set the register options
	runOpts := &nomadapi.RegisterOptions{
		PolicyOverride: false,
		PreserveCounts: false,
		EvalPriority:   0,
	}

	// Submit the job
	_, _, err = d.client.Jobs().RegisterOpts(newJob, runOpts, &nomadapi.WriteOptions{Region: *newJob.Region, Namespace: *newJob.Namespace})
	if err != nil {
		return errors.Wrap(err, "failed to run Nomad job")
	}

	if periodic || paramjob || multiregion {
		if periodic && !paramjob {
			loc, err := newJob.Periodic.GetLocation()
			if err == nil {
				now := time.Now().In(loc)
				if _, err := newJob.Periodic.Next(now); err != nil {
					return errors.Wrap(err, "failed to run Nomad periodic job")
				}
			}
		}
	}

	filesystem.WriteFile(bakFileFolder, bakFileName, newJobFile, 0640)

	return nil
}

// Remove attempts to purge a Nomad job via provided Nomad job file
func (d *Deployer) Remove(ctx context.Context, name string, filePaths []string) error {
	if len(filePaths) == 0 {
		return errors.New("missing Nomad job file paths")
	}
	jobFile, err := filesystem.ReadFromFile(filePaths[0])
	if err != nil {
		return errors.Wrap(err, "failed to read Nomad job file")
	}
	job, err := d.client.Jobs().ParseHCL(string(jobFile), true)
	if err != nil {
		return errors.Wrap(err, "failed to parse Nomad job from file")
	}

	return d.verifyAndPurgeJob(job)
}

func (d *Deployer) verifyAndPurgeJob(job *nomadapi.Job) error {
	// Verify if the job valid, i.e., no error when trying to retrieve job info with the provided job ID
	_, _, err := d.client.Jobs().Info(*job.ID, &nomadapi.QueryOptions{Region: *job.Region, Namespace: *job.Namespace})
	if err != nil {
		// Ignore non-exist job
		errMsg := strings.ToLower(err.Error())
		if strings.Contains(errMsg, "not found") || strings.Contains(errMsg, "404") {
			return nil
		}
		return errors.Wrap(err, "failed to retrieve Nomad job info")
	}

	_, _, err = d.client.Jobs().DeregisterOpts(*job.ID, &nomadapi.DeregisterOptions{Purge: true}, &nomadapi.WriteOptions{Region: *job.Region, Namespace: *job.Namespace})
	if err != nil {
		return errors.Wrap(err, "failed to purge Nomad job")
	}
	return nil
}

// Check if new planning job have crucial differences
func compareJobs(old, new *nomadapi.Job) bool {
	// Check region, namespace and job ID
	if *new.Region != *old.Region ||
		*new.Namespace != *old.Namespace ||
		*new.ID != *old.ID {
		return true
	}

	// Check datacenters
	if len(new.Datacenters) != len(old.Datacenters) {
		return true
	}

	dcMap := make(map[string]bool)

	for _, dc := range new.Datacenters {
		dcMap[dc] = true
	}

	for _, dc := range old.Datacenters {
		if !dcMap[dc] {
			return true
		}
	}

	return false
}
