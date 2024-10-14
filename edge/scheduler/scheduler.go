//go:build !windows
// +build !windows

package scheduler

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/portainer/agent"
	"github.com/portainer/agent/filesystem"

	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog/log"
)

// CronManager is a service that manage schedules by creating a new entry inside the host filesystem under
// the /etc/cron.d folder.
type CronManager struct {
	logsManager      *LogsManager
	cronFileExists   bool
	managedSchedules map[int]agent.Schedule
	cron             *cron.Cron
}

// NewCronManager returns a pointer to a new instance of CronManager.
func NewCronManager(logsManager *LogsManager) *CronManager {
	cron := cron.New()
	cron.Start()

	return &CronManager{
		logsManager:      logsManager,
		cronFileExists:   false,
		managedSchedules: make(map[int]agent.Schedule),
		cron:             cron,
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

		if exists {
			managedSchedule.CollectLogs = schedule.CollectLogs
			manager.managedSchedules[schedule.ID] = managedSchedule
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
	for _, s := range manager.managedSchedules {
		manager.cron.Remove(s.EntryID)
	}

	manager.managedSchedules = map[int]agent.Schedule{}

	return nil
}

func (manager *CronManager) flushEntries(schedules map[int]agent.Schedule) error {
	manager.cron.Stop()
	manager.cron = cron.New()
	manager.cron.Start()

	for key, schedule := range schedules {
		cronSpec, cronEntry, err := createCronEntry(&schedule)
		if err != nil {
			log.Error().Int("schedule_id", schedule.ID).Err(err).Msg("unable to create cron entry")

			return err
		}

		entryID, err := manager.cron.AddFunc(cronSpec, cronEntry)
		if err != nil {
			return err
		}

		s := schedules[key]
		s.EntryID = entryID
		schedules[key] = s
	}

	log.Debug().Int("schedule_count", len(manager.managedSchedules)).Msg("writing cron file on disk")

	manager.cronFileExists = true
	manager.managedSchedules = schedules

	return nil
}

func createCronEntry(schedule *agent.Schedule) (string, cron.FuncJob, error) {
	decodedScript, err := base64.RawStdEncoding.DecodeString(schedule.Script)
	if err != nil {
		return "", nil, err
	}

	err = filesystem.WriteFile(fmt.Sprintf("%s%s", agent.HostRoot, agent.ScheduleScriptDirectory), fmt.Sprintf("schedule_%d", schedule.ID), decodedScript, 0744)
	if err != nil {
		return "", nil, err
	}

	cronExpression := schedule.CronExpression
	command := fmt.Sprintf("%s/schedule_%d", agent.ScheduleScriptDirectory, schedule.ID)
	logFile := fmt.Sprintf("%s/schedule_%d.log", agent.ScheduleScriptDirectory, schedule.ID)

	cronFn := func() {
		log.Info().Str("command", command).Msg("running cron job")

		logFileWriter, err := os.OpenFile(filepath.Join(agent.HostRoot, logFile), os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			log.Error().Err(err).Msg("could not open the log file")
			return
		}
		defer logFileWriter.Close()

		cmd := exec.Command("/bin/sh", command)
		cmd.Dir = "/"
		cmd.SysProcAttr = &syscall.SysProcAttr{Chroot: agent.HostRoot}
		cmd.Env = []string{
			"SHELL=/bin/sh",
			"PATH=/usr/local/sbin:/usr/local/bin:/sbin:/bin:/usr/sbin:/usr/bin",
		}
		cmd.Stdout = logFileWriter
		cmd.Stderr = logFileWriter

		if err := cmd.Run(); err != nil {
			log.Error().Err(err).Msg("error encountered in cron job run")
		}
	}

	return cronExpression, cronFn, nil
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

	return manager.flushEntries(manager.managedSchedules)
}
