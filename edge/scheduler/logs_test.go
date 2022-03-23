package scheduler

import "testing"

func TestDataRace(t *testing.T) {
	m := NewLogsManager("portainerURL", "endpointID", "edgeID", true)
	m.Start()
	m.HandleReceivedLogsRequests([]int{1})
}
