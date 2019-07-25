// +build windows

package filesystem

import "github.com/portainer/agent"

type CronManager struct {
}

func NewCronManager() *CronManager {
	return &CronManager{}
}

func (manager *CronManager) Schedule(schedules []agent.Schedule) error {
	return nil
}
