//go:build !windows
// +build !windows

package scheduler

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/portainer/agent"
	"github.com/portainer/agent/filesystem"

	"github.com/rs/zerolog/log"
)

const (
	cronDirectory = "/etc/cron.d"
	cronFile      = "portainer_agent"
	cronJobUser   = "root"
)

// CronManager is a service that manage schedules by creating a new entry inside the host filesystem under
// the /etc/cron.d folder.
type CronManager struct {
	logsManager      *LogsManager
	cronFileExists   bool
	managedSchedules map[int]agent.Schedule
}

// NewCronManager returns a pointer to a new instance of CronManager.
func NewCronManager(logsManager *LogsManager) *CronManager {
	return &CronManager{
		logsManager:      logsManager,
		cronFileExists:   false,
		managedSchedules: make(map[int]agent.Schedule),
	}
}

// Schedule takes care of writing schedules on disk inside a cron file.
// It also creates/updates the script associated to each schedule on the filesystem.
// It keeps track of managed schedules and will flush the content of the cron file only if it detects any change.
// Note that this implementation do not clean up scripts located on the filesystem that are related to old schedules.
func (manager *CronManager) Schedule(schedules []agent.Schedule) error {
	schedulesMap := map[int]agent.Schedule{}
	for _, schedule := range schedules {
		schedulesMap[schedule.ID] = schedule
	}

	if len(schedules) == 0 {
		return manager.removeCronFile()
	}

	collectLogs := false
	updateRequired := false

	for _, schedule := range schedules {
		managedSchedule, exists := manager.managedSchedules[schedule.ID]
		if exists && managedSchedule.Version != schedule.Version {
			log.Debug().
				Int("schedule_id", schedule.ID).
				Int("version", schedule.Version).
				Msg("found schedule with new version")

			updateRequired = true
		} else if !exists {
			log.Debug().
				Int("schedule_id", schedule.ID).
				Int("version", schedule.Version).
				Msg("found a new schedule")

			updateRequired = true
		}

		if schedule.CollectLogs {
			log.Debug().
				Int("schedule_id", schedule.ID).
				Int("version", schedule.Version).
				Msg("found schedule with logs to collect")

			collectLogs = true
		}
	}

	if updateRequired {
		if err := manager.flushEntries(schedulesMap); err != nil {
			return err
		}
	}

	if collectLogs {
		manager.ProcessScheduleLogsCollection()
	}

	return nil
}

func (manager *CronManager) removeCronFile() error {
	manager.managedSchedules = map[int]agent.Schedule{}
	if manager.cronFileExists {
		log.Debug().Msg("no schedules available, removing cron file")

		manager.cronFileExists = false
		return filesystem.RemoveFile(fmt.Sprintf("%s%s/%s", agent.HostRoot, cronDirectory, cronFile))
	}
	return nil
}

func (manager *CronManager) flushEntries(schedules map[int]agent.Schedule) error {
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
			log.Error().Int("schedule_id", schedule.ID).Err(err).Msg("unable to create cron entry")

			return err
		}

		cronEntries = append(cronEntries, cronEntry)
	}

	log.Debug().Int("schedule_count", len(manager.managedSchedules)).Msg("writing cron file on disk")

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

func createCronEntry(schedule *agent.Schedule) (string, error) {
	decodedScript, err := base64.RawStdEncoding.DecodeString(schedule.Script)
	if err != nil {
		return "", err
	}

	err = filesystem.WriteFile(fmt.Sprintf("%s%s", agent.HostRoot, agent.ScheduleScriptDirectory), fmt.Sprintf("schedule_%d", schedule.ID), decodedScript, 0744)
	if err != nil {
		return "", err
	}

	cronExpression := schedule.CronExpression
	command := fmt.Sprintf("%s/schedule_%d", agent.ScheduleScriptDirectory, schedule.ID)
	logFile := fmt.Sprintf("%s/schedule_%d.log", agent.ScheduleScriptDirectory, schedule.ID)

	return fmt.Sprintf("%s %s %s > %s 2>&1", cronExpression, cronJobUser, command, logFile), nil
}

func (manager *CronManager) ProcessScheduleLogsCollection() {
	logsToCollect := []int{}

	for _, schedule := range manager.managedSchedules {
		if schedule.CollectLogs {
			logsToCollect = append(logsToCollect, schedule.ID)
			schedule.CollectLogs = false
		}
	}

	manager.logsManager.HandleReceivedLogsRequests(logsToCollect)
}

func (manager *CronManager) AddSchedule(schedule agent.Schedule) error {
	manager.managedSchedules[schedule.ID] = schedule

	return manager.flushEntries(manager.managedSchedules)
}

func (manager *CronManager) RemoveSchedule(schedule agent.Schedule) error {
	delete(manager.managedSchedules, schedule.ID)

	if len(manager.managedSchedules) == 0 {
		return manager.removeCronFile()
	}

	return manager.flushEntries(manager.managedSchedules)
}
