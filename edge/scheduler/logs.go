package scheduler

import (
	"fmt"
	"log"

	"github.com/portainer/agent"
	"github.com/portainer/agent/edge/client"
	"github.com/portainer/agent/filesystem"
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
	log.Printf("[DEBUG] [edge,scheduler] [message: logs manager started]")
	go manager.loop()
}

func (manager *LogsManager) loop() {
	for {
		for _, jobID := range <-manager.jobsCh {
			log.Printf("[DEBUG] [edge,scheduler] [job_identifier: %d] [message: started job log collection]", jobID)

			logFileLocation := fmt.Sprintf("%s%s/schedule_%d.log", agent.HostRoot, agent.ScheduleScriptDirectory, jobID)
			exist, err := filesystem.FileExists(logFileLocation)
			if err != nil {
				log.Printf("[ERROR] [edge,scheduler] [error: %s] [message: Failed fetching log file]", err)
				continue
			}

			var file []byte
			if !exist {
				file = []byte("")
				log.Printf("[DEBUG] [edge,scheduler] [job_identifier: %d] [message: file doesn't exist]", jobID)
			} else {
				file, err = filesystem.ReadFromFile(logFileLocation)
				if err != nil {
					log.Printf("[ERROR] [edge,scheduler] [error: %s] [message: Failed fetching log file]", err)
					continue
				}
			}

			edgeJobStatus := agent.EdgeJobStatus{
				JobID:          jobID,
				LogFileContent: string(file),
			}
			err = manager.portainerClient.SetEdgeJobStatus(edgeJobStatus)
			if err != nil {
				log.Printf("[ERROR] [edge,scheduler] [error: %s] [message: Failed sending log file to portainer]", err)
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
