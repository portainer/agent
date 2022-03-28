package scheduler

import "testing"

func TestDataRace(t *testing.T) {
	m := NewLogsManager(nil)
	m.Start()
	m.HandleReceivedLogsRequests([]int{1})
}
