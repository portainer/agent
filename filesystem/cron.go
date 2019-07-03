package filesystem

import (
	"encoding/base64"
	"fmt"
	"log"
	"strings"

	"github.com/portainer/agent"
)

// TODO: Document
type CronManager struct {
}

func NewCronManager() *CronManager {
	return &CronManager{}
}

// TODO: DOC
// Note that this implementation do not clean-up scripts related to old schedules
// This implementation should be optimized too, it will always write file on disks every time it is called
// Will create a file in cron directory with the following header
// ## This file is managed by Portainer agent. DO NOT EDIT MANUALLY.
// SHELL=/bin/sh
// PATH=/usr/local/sbin:/usr/local/bin:/sbin:/bin:/usr/sbin:/usr/bin
func (manager *CronManager) Schedule(schedules []agent.Schedule) error {
	if len(schedules) == 0 {
		// TODO: deliberately skip error to avoid "remove /host/etc/cron.d/portainer_agent: no such file or directory"
		// for optimization purposes manager should have a variable to keep track of an existing file
		RemoveFile(fmt.Sprintf("%s%s/%s", agent.HostRoot, agent.CronDirectory, agent.CronFile))
		return nil
	}

	cronEntries := []string{
		"## This file is managed by Portainer agent. DO NOT EDIT MANUALLY.",
		"SHELL=/bin/sh",
		"PATH=/usr/local/sbin:/usr/local/bin:/sbin:/bin:/usr/sbin:/usr/bin",
		"",
	}

	for _, schedule := range schedules {
		cronEntry, err := createCronEntry(&schedule)
		if err != nil {
			log.Printf("[ERROR] [filesystem,cron] [schedule_id: %d] [message: Unable to create cron entry] [err: %s]", schedule.ID, err)
			continue
		}
		cronEntries = append(cronEntries, cronEntry)
	}

	cronEntries = append(cronEntries, "")
	cronFileContent := strings.Join(cronEntries, "\n")
	return WriteFile(fmt.Sprintf("%s%s", agent.HostRoot, agent.CronDirectory), agent.CronFile, []byte(cronFileContent), 0644)
}

func createCronEntry(schedule *agent.Schedule) (string, error) {
	decodedScript, err := base64.RawStdEncoding.DecodeString(schedule.Script)
	if err != nil {
		return "", err
	}

	err = WriteFile(fmt.Sprintf("%s%s", agent.HostRoot, agent.ScheduleScriptDirectory), fmt.Sprintf("schedule_%d", schedule.ID), []byte(decodedScript), 0744)
	if err != nil {
		return "", err
	}

	cronExpression := schedule.CronExpression
	command := fmt.Sprintf("%s/schedule_%d", agent.ScheduleScriptDirectory, schedule.ID)
	logFile := fmt.Sprintf("%s/schedule_%d.log", agent.ScheduleScriptDirectory, schedule.ID)

	return fmt.Sprintf("%s %s %s > %s 2>&1", cronExpression, agent.CronUser, command, logFile), nil
}
