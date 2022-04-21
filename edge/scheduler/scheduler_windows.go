//go:build windows
// +build windows

package scheduler

import "github.com/portainer/agent"

type CronManager struct {
}

func NewCronManager(logsManager *LogsManager) *CronManager {
	return &CronManager{}
}

func (manager *CronManager) Schedule(schedules []agent.Schedule) error {
	return nil
}

func (manager *CronManager) AddSchedule(schedule agent.Schedule) error {
	return nil
}

func (manager *CronManager) RemoveSchedule(schedule agent.Schedule) error {
	return nil
}

func (manager *CronManager) ProcessScheduleLogsCollection() {
}
