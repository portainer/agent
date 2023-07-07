package nomad

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	nomadapi "github.com/hashicorp/nomad/api"
	"github.com/pkg/errors"
	"github.com/portainer/agent"
	"github.com/portainer/agent/filesystem"
	agentos "github.com/portainer/agent/os"
	"github.com/portainer/portainer/pkg/libstack"
	"github.com/rs/zerolog/log"
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
func (d *Deployer) Deploy(ctx context.Context, name string, filePaths []string, options agent.DeployOptions) error {
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
		diff := compareJobs(newJob, oldJob)
		if diff {
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

	// Check if this is a portainer-updater job
	if isUpdateJob(newJob) {
		// If if this is a portainer-updater job
		// Purge the old job before register the new one
		err = d.verifyAndPurgeJob(newJob)
		if err != nil {
			return errors.Wrap(err, "failed to purge former Nomad job")
		}
		addNomadDefaultEnv(newJob)
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

// Pull is a dummy method for Nomad
func (d *Deployer) Pull(ctx context.Context, name string, filePaths []string, options agent.PullOptions) error {
	return nil
}

// Validate is a dummy method for Nomad
func (d *Deployer) Validate(ctx context.Context, name string, filePaths []string, options agent.ValidateOptions) error {
	// We can use PlanOpts() to validate the HCL file and see if the file is valid
	return nil
}

// Remove attempts to purge a Nomad job via provided Nomad job file
func (d *Deployer) Remove(ctx context.Context, name string, filePaths []string, options agent.RemoveOptions) error {
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

// addNomadDefaultEnv injects environment varibles inherited from Nomad environment
func addNomadDefaultEnv(job *nomadapi.Job) {
	task := job.TaskGroups[0].Tasks[0]

	// Inject Nomad environment variables only when the custom env "PORTAINER_UPDATER"
	// is configured as Nomad update job environment variable
	_, ok := task.Env[agent.PortainerUpdaterEnv]
	if !ok {
		log.Debug().Msg("fail to look up the custom env PORTAINER_UPDATE")
		return
	}

	if task.Env == nil {
		task.Env = make(map[string]string)
	}

	// By injecting the below environment variables, Nomad SDK client can
	// be initialized correctly, which is able to communicate with Nomad
	// API from another Nomad job "portainer-updater"
	task.Env[agent.NomadAddrEnvVarName] = os.Getenv(agent.NomadAddrEnvVarName)
	task.Env[agent.NomadRegionEnvVarName] = os.Getenv(agent.NomadRegionEnvVarName)
	task.Env[agent.NomadNamespaceEnvVarName] = os.Getenv(agent.NomadNamespaceEnvVarName)
	task.Env[agent.NomadTokenEnvVarName] = os.Getenv(agent.NomadTokenEnvVarName)

	// Inject Nomad TLS certificate info to updater job if the TLS
	// certificates are provided
	nomadCaCert, exist := os.LookupEnv(agent.NomadCACertContentEnvVarName)
	if exist {
		// The nomad agent has configured TLS certificate
		task.Env[agent.NomadCACertContentEnvVarName] = nomadCaCert
	}
	nomadClientCert, exist := os.LookupEnv(agent.NomadClientCertContentEnvVarName)
	if exist {
		task.Env[agent.NomadClientCertContentEnvVarName] = nomadClientCert
	}
	nomadClientKey, exist := os.LookupEnv(agent.NomadClientKeyContentEnvVarName)
	if exist {
		task.Env[agent.NomadClientKeyContentEnvVarName] = nomadClientKey
	}

	// Inject portainer agent env
	task.Env[agentos.EnvKeyEdge] = os.Getenv(agentos.EnvKeyEdge)
	task.Env[agentos.EnvKeyEdgeKey] = os.Getenv(agentos.EnvKeyEdgeKey)
	task.Env[agentos.EnvKeyEdgeID] = os.Getenv(agentos.EnvKeyEdgeID)
	task.Env[agentos.EnvKeyEdgeInsecurePoll] = os.Getenv(agentos.EnvKeyEdgeInsecurePoll)
	task.Env[agentos.EnvKeyAgentSecret] = os.Getenv(agentos.EnvKeyAgentSecret)

	job.TaskGroups[0].Tasks[0] = task
}

func isUpdateJob(job *nomadapi.Job) bool {
	targetJobName := "portainer-updater"
	return *job.ID == targetJobName &&
		len(job.TaskGroups) > 0 &&
		*job.TaskGroups[0].Name == targetJobName &&
		len(job.TaskGroups[0].Tasks) > 0 &&
		job.TaskGroups[0].Tasks[0].Name == targetJobName
}

func (service *Deployer) WaitForStatus(ctx context.Context, name string, status libstack.Status) <-chan string {
	resultCh := make(chan string)

	close(resultCh)

	return resultCh
}
