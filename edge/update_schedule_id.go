package edge

import (
	"log"
	"os"
	"strconv"
)

const (
	envKeyUpdateScheduleID = "UPDATE_SCHEDULE_ID"
)

func getUpdateScheduleID() int {
	str := os.Getenv(envKeyUpdateScheduleID)
	if str == "" {
		return 0
	}

	id, err := strconv.Atoi(str)
	if err != nil {
		log.Printf("[WARN] [edge, client] [message: parsing %s failed] [error: %s]", envKeyUpdateScheduleID, err)
		return 0
	}

	return id
}
