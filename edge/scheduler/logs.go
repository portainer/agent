package scheduler

import (
	"fmt"

	"github.com/portainer/agent"
	"github.com/portainer/agent/edge/client"
	"github.com/portainer/agent/filesystem"

	"github.com/rs/zerolog/log"
)

type LogsManager struct {
	portainerClient client.PortainerClient
	jobsCh          chan []int
}

func NewLogsManager(cli client.PortainerClient) *LogsManager {
	return &LogsManager{
		portainerClient: cli,
		jobsCh:          make(chan []int),
	}
}

func (manager *LogsManager) Start() {
	log.Debug().Msg("logs manager started")

	go manager.loop()
}

func (manager *LogsManager) loop() {
	for {
		for _, jobID := range <-manager.jobsCh {
			log.Debug().Int("job_identifier", jobID).Msg("started job log collection")

			logFileLocation := fmt.Sprintf("%s%s/schedule_%d.log", agent.HostRoot, agent.ScheduleScriptDirectory, jobID)
			exist, err := filesystem.FileExists(logFileLocation)
			if err != nil {
				log.Error().Err(err).Msg("failed fetching log file")

				continue
			}

			var file []byte
			if !exist {
				file = []byte("")

				log.Debug().Int("job_identifier", jobID).Msg("file doesn't exist")
			} else {
				file, err = filesystem.ReadFromFile(logFileLocation)
				if err != nil {
					log.Error().Err(err).Msg("failed fetching log file")

					continue
				}
			}

			edgeJobStatus := agent.EdgeJobStatus{
				JobID:          jobID,
				LogFileContent: string(file),
			}
			err = manager.portainerClient.SetEdgeJobStatus(edgeJobStatus)
			if err != nil {
				log.Error().Err(err).Msg("failed sending log file to portainer")

				continue
			}
		}
	}
}

func (manager *LogsManager) HandleReceivedLogsRequests(jobs []int) {
	if len(jobs) > 0 {
		manager.jobsCh <- jobs
	}
}
