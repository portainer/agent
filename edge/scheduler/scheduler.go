//go:build !windows
// +build !windows

package scheduler

import (
	"encoding/base64"
	"fmt"
	"log"
	"strings"

	"github.com/portainer/agent"
	"github.com/portainer/agent/filesystem"
)

const (
	cronDirectory = "/etc/cron.d"
	cronFile      = "portainer_agent"
	cronJobUser   = "root"
)

// CronManager is a service that manage schedules by creating a new entry inside the host filesystem under
// the /etc/cron.d folder.
type CronManager struct {
	cronFileExists   bool
	managedSchedules []agent.Schedule
}

// NewCronManager returns a pointer to a new instance of CronManager.
func NewCronManager() *CronManager {
	return &CronManager{
		cronFileExists:   false,
		managedSchedules: make([]agent.Schedule, 0),
	}
}

// Schedule takes care of writing schedules on disk inside a cron file.
// It also creates/updates the script associated to each schedule on the filesystem.
// It keeps track of managed schedules and will flush the content of the cron file only if it detects any change.
// Note that this implementation do not clean-up scripts located on the filesystem that are related to old schedules.
func (manager *CronManager) Schedule(schedules []agent.Schedule) error {
	if len(schedules) == 0 {
		manager.managedSchedules = schedules
		if manager.cronFileExists {
			log.Println("[DEBUG] [edge,scheduler] [message: no schedules available, removing cron file]")
			manager.cronFileExists = false
			return filesystem.RemoveFile(fmt.Sprintf("%s%s/%s", agent.HostRoot, cronDirectory, cronFile))
		}
		return nil
	}

	if len(manager.managedSchedules) != len(schedules) {
		return manager.flushEntries(schedules)
	}

	updateRequired := false
	for _, schedule := range schedules {
		for _, managed := range manager.managedSchedules {
			if schedule.ID == managed.ID && schedule.Version != managed.Version {
				log.Printf("[DEBUG] [edge,scheduler] [schedule_id: %d] [version: %d] [message: Found schedule with new version]", schedule.ID, schedule.Version)
				updateRequired = true
				break
			}
		}

		if updateRequired {
			break
		}
	}

	if updateRequired {
		return manager.flushEntries(schedules)
	}

	return nil
}

func createCronEntry(schedule *agent.Schedule) (string, error) {
	decodedScript, err := base64.RawStdEncoding.DecodeString(schedule.Script)
	if err != nil {
		return "", err
	}

	err = filesystem.WriteFile(fmt.Sprintf("%s%s", agent.HostRoot, agent.ScheduleScriptDirectory), fmt.Sprintf("schedule_%d", schedule.ID), []byte(decodedScript), 0744)
	if err != nil {
		return "", err
	}

	cronExpression := schedule.CronExpression
	command := fmt.Sprintf("%s/schedule_%d", agent.ScheduleScriptDirectory, schedule.ID)
	logFile := fmt.Sprintf("%s/schedule_%d.log", agent.ScheduleScriptDirectory, schedule.ID)

	return fmt.Sprintf("%s %s %s > %s 2>&1", cronExpression, cronJobUser, command, logFile), nil
}

func (manager *CronManager) flushEntries(schedules []agent.Schedule) error {
	cronEntries := make([]string, 0)

	header := []string{
		"## This file is managed by the Portainer agent. DO NOT EDIT MANUALLY ALL YOUR CHANGES WILL BE OVERWRITTEN.",
		"SHELL=/bin/sh",
		"PATH=/usr/local/sbin:/usr/local/bin:/sbin:/bin:/usr/sbin:/usr/bin",
		"",
	}

	cronEntries = append(cronEntries, header...)

	for _, schedule := range schedules {
		cronEntry, err := createCronEntry(&schedule)
		if err != nil {
			log.Printf("[ERROR] [edge,scheduler] [schedule_id: %d] [message: Unable to create cron entry] [err: %s]", schedule.ID, err)
			return err
		}
		cronEntries = append(cronEntries, cronEntry)
	}

	log.Printf("[DEBUG] [edge,scheduler] [schedule_count: %d] [message: Writing cron file on disk]", len(manager.managedSchedules))

	cronEntries = append(cronEntries, "")
	cronFileContent := strings.Join(cronEntries, "\n")
	err := filesystem.WriteFile(fmt.Sprintf("%s%s", agent.HostRoot, cronDirectory), cronFile, []byte(cronFileContent), 0644)
	if err != nil {
		return err
	}

	manager.cronFileExists = true
	manager.managedSchedules = schedules

	return nil
}
