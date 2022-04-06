package scheduler

import "testing"

func TestDataRace(t *testing.T) {
	endpointFn := func() string { return "1" }
	m := NewLogsManager("portainerURL", "edgeID", endpointFn, true)
	m.Start()
	m.HandleReceivedLogsRequests([]int{1})
}
