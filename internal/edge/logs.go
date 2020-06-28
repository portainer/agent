package edge

import (
	"fmt"
	"log"
	"time"

	"github.com/portainer/agent"
	"github.com/portainer/agent/filesystem"
	"github.com/portainer/agent/http/client"
)

type logsManager struct {
	httpClient *client.PortainerClient
	stopSignal chan struct{}
	jobs       map[int]logStatus
}

type logStatus int

const (
	_ logStatus = iota
	logPending
	logSuccess
	logFailed
)

func newLogsManager(portainerURL, endpointID, edgeID string) *logsManager {
	cli := client.NewPortainerClient(portainerURL, endpointID, edgeID)

	return &logsManager{
		httpClient: cli,
		stopSignal: nil,
		jobs:       map[int]logStatus{},
	}
}

func (manager *logsManager) start() error {
	if manager.stopSignal != nil {
		return nil
	}

	manager.stopSignal = make(chan struct{})

	queueSleepInterval, err := time.ParseDuration(agent.EdgeStackQueueSleepInterval)
	if err != nil {
		return err
	}
	log.Printf("[DEBUG] [internal,edge,logs] [message: logs manager started]")

	go func() {
		for {
			select {
			case <-manager.stopSignal:
				log.Println("[DEBUG] [internal,edge,logs] [message: shutting down Edge logs manager]")
				return
			default:
				jobID := manager.next()
				if jobID == 0 {
					timer := time.NewTimer(queueSleepInterval)
					<-timer.C
					continue
				}

				log.Printf("[DEBUG] [internal,edge,logs] [job_identifier: %d] [message: started job log collection]", jobID)

				logFileLocation := fmt.Sprintf("%s%s/schedule_%d.log", agent.HostRoot, agent.ScheduleScriptDirectory, jobID)
				exist, err := filesystem.FileExists(logFileLocation)
				if err != nil {
					manager.jobs[jobID] = logFailed
					log.Printf("[ERROR] [internal,edge,logs] [error: %s] [message: Failed fetching log file]", err)
					continue
				}

				var file []byte
				if !exist {
					file = []byte("")
					log.Printf("[DEBUG] [internal,edge,logs] [job_identifier: %d] [message: file doesn't exist]", jobID)
				} else {
					file, err = filesystem.ReadFromFile(logFileLocation)
					if err != nil {
						manager.jobs[jobID] = logFailed
						log.Printf("[ERROR] [internal,edge,logs] [error: %s] [message: Failed fetching log file]", err)
						continue
					}
				}

				err = manager.httpClient.SendJobLogFile(jobID, file)
				if err != nil {
					manager.jobs[jobID] = logFailed
					log.Printf("[ERROR] [internal,edge,logs] [error: %s] [message: Failed sending log file to portainer]", err)
					continue
				}

				delete(manager.jobs, jobID)
			}
		}
	}()

	return nil
}

func (manager *logsManager) stop() {
	if manager.stopSignal != nil {
		log.Printf("[DEBUG] [internal,edge,logs] [message: logs manager stopped]")
		close(manager.stopSignal)
		manager.stopSignal = nil
	}
}

func (manager *logsManager) handleReceivedLogsRequests(jobs []int) {
	for _, jobID := range jobs {
		if _, ok := manager.jobs[jobID]; !ok {
			log.Printf("[DEBUG] [internal,edge,logs] [job_identifier: %d] [message: added job to queue]", jobID)
			manager.jobs[jobID] = logPending
		}
	}
}

func (manager *logsManager) next() int {
	for jobID, status := range manager.jobs {
		if status == logPending {
			return jobID
		}
	}
	return 0
}
