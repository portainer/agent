package edge

import (
	"fmt"
	"log"

	"github.com/portainer/agent"
	"github.com/portainer/agent/filesystem"
	"github.com/portainer/agent/http/client"
)

type logsManager struct {
	httpClient *client.PortainerClient
	jobsCh     chan []int
}

func newLogsManager(portainerURL, endpointID, edgeID string, insecurePoll bool, tunnel bool) *logsManager {
	cli := client.NewPortainerClient(portainerURL, endpointID, edgeID, insecurePoll, tunnel)

	return &logsManager{
		httpClient: cli,
		jobsCh:     make(chan []int),
	}
}

func (manager *logsManager) start() {
	log.Printf("[DEBUG] [edge,scheduler] [message: logs manager started]")
	go manager.loop()
}

func (manager *logsManager) loop() {
	for {
		for _, jobID := range <-manager.jobsCh {
			log.Printf("[DEBUG] [internal,edge,logs] [job_identifier: %d] [message: started job log collection]", jobID)

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
					log.Printf("[ERROR] [internal,edge,logs] [error: %s] [message: Failed fetching log file]", err)
					continue
				}
			}

			err = manager.httpClient.SendJobLogFile(jobID, file)
			if err != nil {
				log.Printf("[ERROR] [internal,edge,logs] [error: %s] [message: Failed sending log file to portainer]", err)
				continue
			}
		}
	}
}

func (manager *logsManager) handleReceivedLogsRequests(jobs []int) {
	if len(jobs) > 0 {
		manager.jobsCh <- jobs
	}
}
