package client

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/portainer/agent"
	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/pkg/fips"

	"github.com/stretchr/testify/require"
	"github.com/wI2L/jsondiff"
)

func Test_executeAsyncRequestCompression(t *testing.T) {
	t.Parallel()
	fips.InitFIPS(false)

	client := &PortainerAsyncClient{
		getEndpointIDFn: func() portainer.EndpointID { return 1 },
		httpClient:      BuildHTTPClient(30, &agent.Options{}),
	}

	// Small payload, no compression expected
	payload := AsyncRequest{Snapshot: &snapshot{}}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Encoding") == "gzip" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()

	_, err := client.executeAsyncRequest(payload, srv.URL)
	require.NoError(t, err)

	// Large payload, compression expected
	payload = AsyncRequest{Snapshot: &snapshot{}}
	payload.Snapshot.DockerPatch = make([]jsondiff.Operation, 100)

	for i := range payload.Snapshot.DockerPatch {
		payload.Snapshot.DockerPatch[i] = jsondiff.Operation{
			Type: jsondiff.OperationAdd,
		}
	}

	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Encoding") != "gzip" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()

	_, err = client.executeAsyncRequest(payload, srv.URL)
	require.NoError(t, err)
}

func TestCommandPollingResiliency(t *testing.T) {
	t.Parallel()
	fips.InitFIPS(false)

	cmdID := 1
	var asyncCmds []AsyncCommand
	var lastSentCommandTimestamp time.Time

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req AsyncRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.CommandTimestamp == nil {
			w.WriteHeader(http.StatusBadRequest)

			return
		}

		lastSentCommandTimestamp = *req.CommandTimestamp

		// Always append a new command to the list
		asyncCmds = append(asyncCmds, AsyncCommand{
			ID:        cmdID,
			Type:      "edgeStack",
			Timestamp: time.Now(),
			Operation: "add",
		})

		resp := &AsyncResponse{EndpointID: 1, Commands: asyncCmds}

		cmdID++

		if err := json.NewEncoder(w).Encode(resp); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	// Setup Portainer Async Client
	cli := NewPortainerAsyncClient(
		srv.URL,
		func(id portainer.EndpointID) {},
		func() portainer.EndpointID { return 1 },
		"test-edge-id",
		"invalid-edge-key",
		agent.PlatformDocker,
		agent.EdgeMetaFields{},
		BuildHTTPClient(30, &agent.Options{}),
	)

	// Poll
	resp, err := cli.GetEnvironmentStatus("command")
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.AsyncCommands, 1)

	// Simulate the command being processed
	cli.SetLastCommandTimestamp(asyncCmds[0].Timestamp)
	require.True(t, asyncCmds[0].Timestamp.Equal(cli.commandTimestamp))

	cli.SetPendingCommand(1, 1, asyncCmds[0].Timestamp)
	require.Len(t, cli.pendingESCommandsTS, 1)

	// Make sure only the new command is returned
	resp, err = cli.GetEnvironmentStatus("command")
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.AsyncCommands, 1)

	// Simulate the command being processed
	cli.SetLastCommandTimestamp(asyncCmds[1].Timestamp)
	require.True(t, asyncCmds[1].Timestamp.Equal(cli.commandTimestamp))

	cli.SetPendingCommand(2, 1, asyncCmds[1].Timestamp)
	require.Len(t, cli.pendingESCommandsTS, 2)

	// Check that command timestamp is greater than the sent one when there are pending commands
	require.True(t, cli.commandTimestamp.After(lastSentCommandTimestamp))

	// Simulate different version being processed
	err = cli.SetEdgeStackStatus(1, 2, portainer.EdgeStackStatusRunning, nil, "")
	require.NoError(t, err)
	require.Len(t, cli.pendingESCommandsTS, 2)

	// Simulate all commands being processed properly
	err = cli.SetEdgeStackStatus(1, 1, portainer.EdgeStackStatusRunning, nil, "")
	require.NoError(t, err)

	err = cli.SetEdgeStackStatus(2, 1, portainer.EdgeStackStatusRunning, nil, "")
	require.NoError(t, err)

	require.Empty(t, cli.pendingESCommandsTS)

	// Check that the command timestamp is equal to the last sent one when there are no pending commands
	resp, err = cli.GetEnvironmentStatus("command")
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.AsyncCommands, 1)
	require.True(t, lastSentCommandTimestamp.Equal(cli.commandTimestamp))
}

func TestResyncRequest(t *testing.T) {
	t.Parallel()
	fips.InitFIPS(false)

	var lastResync bool
	var resyncCount int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req AsyncRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.CommandTimestamp == nil {
			w.WriteHeader(http.StatusBadRequest)

			return
		}

		lastResync = req.Resync
		if req.Resync {
			resyncCount++
		}

		resp := &AsyncResponse{EndpointID: 1}

		if err := json.NewEncoder(w).Encode(resp); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	cli := NewPortainerAsyncClient(
		srv.URL,
		func(id portainer.EndpointID) {},
		func() portainer.EndpointID { return 1 },
		"test-edge-id",
		"invalid-edge-key",
		agent.PlatformDocker,
		agent.EdgeMetaFields{},
		BuildHTTPClient(30, &agent.Options{}),
	)

	require.True(t, cli.needsResync)

	// First command-flagged poll must request a resync, and clear the flag on success
	_, err := cli.GetEnvironmentStatus("command")
	require.NoError(t, err)
	require.True(t, lastResync)
	require.False(t, cli.needsResync)

	// Subsequent polls must not resend the resync request
	_, err = cli.GetEnvironmentStatus("command")
	require.NoError(t, err)
	require.False(t, lastResync)
	require.Equal(t, 1, resyncCount)
}

func TestIsDockerSnapshotDiffEmpty(t *testing.T) {
	t.Parallel()
	// Empty cases

	emptyPatches := [][]jsondiff.Operation{
		// Noisy field regexps
		{
			{
				Path: "/DockerSnapshotRaw/Containers/1/Status",
				Type: "add",
			},
		},

		// Noisy fields
		{
			{
				Path: "/Time",
				Type: "replace",
			},
		},
		{
			{
				Path: "/DockerSnapshotRaw/Info/NEventsListener",
				Type: "replace",
			},
		},
	}

	for _, patch := range emptyPatches {
		require.True(t, isDockerSnapshotDiffEmpty(patch))
	}

	// Non-empty cases

	nonEmptyPatches := [][]jsondiff.Operation{
		// Non-noisy field
		{
			{
				Path: "/DockerSnapshotRaw/Containers/1/Image",
				Type: "add",
			},
		},
	}

	for _, patch := range nonEmptyPatches {
		require.False(t, isDockerSnapshotDiffEmpty(patch))
	}
}

func TestCreateDockerSnapshotSkipsWhenSnapshotUnchanged(t *testing.T) {
	t.Parallel()

	dockerSnapshot := &portainer.DockerSnapshot{ContainerCount: 3}

	client := &PortainerAsyncClient{
		createSnapshotFn: func(string) (*portainer.DockerSnapshot, error) { return dockerSnapshot, nil },
	}
	client.lastSnapshot.Docker = dockerSnapshot

	payload := &AsyncRequest{Snapshot: &snapshot{}}
	var currentSnapshot snapshot

	client.createDockerSnapshot(payload, &currentSnapshot)

	require.Nil(t, payload.Snapshot.Docker)
	require.Nil(t, payload.Snapshot.DockerPatch)
	require.Nil(t, payload.Snapshot.DockerHash)
	require.Same(t, dockerSnapshot, currentSnapshot.Docker)
}

func TestCreateDockerSnapshotGeneratesPatchAndHash(t *testing.T) {
	t.Parallel()

	lastDocker := &portainer.DockerSnapshot{ContainerCount: 1}
	newDocker := &portainer.DockerSnapshot{ContainerCount: 2}

	client := &PortainerAsyncClient{
		createSnapshotFn: func(string) (*portainer.DockerSnapshot, error) { return newDocker, nil },
	}
	client.lastSnapshot.Docker = lastDocker

	payload := &AsyncRequest{Snapshot: &snapshot{}}
	var currentSnapshot snapshot

	client.createDockerSnapshot(payload, &currentSnapshot)

	require.Nil(t, payload.Snapshot.Docker)
	require.Nil(t, currentSnapshot.Docker)
	require.NotEmpty(t, payload.Snapshot.DockerPatch)
	require.NotNil(t, payload.Snapshot.DockerHash)
}

func TestCreateDockerSnapshotSkipsPatchWhenLastSnapshotMarshalFails(t *testing.T) {
	t.Parallel()

	lastDocker := &portainer.DockerSnapshot{PerformanceMetrics: &portainer.PerformanceMetrics{CPUUsage: math.NaN()}}
	newDocker := &portainer.DockerSnapshot{ContainerCount: 2}

	client := &PortainerAsyncClient{
		createSnapshotFn: func(string) (*portainer.DockerSnapshot, error) { return newDocker, nil },
	}
	client.lastSnapshot.Docker = lastDocker

	payload := &AsyncRequest{Snapshot: &snapshot{}}
	var currentSnapshot snapshot

	client.createDockerSnapshot(payload, &currentSnapshot)

	require.Same(t, newDocker, payload.Snapshot.Docker)
	require.Nil(t, payload.Snapshot.DockerPatch)
	require.Nil(t, payload.Snapshot.DockerHash)
}

func TestCreateDockerSnapshotSkipsPatchWhenCurrentSnapshotMarshalFails(t *testing.T) {
	t.Parallel()

	lastDocker := &portainer.DockerSnapshot{ContainerCount: 1}
	newDocker := &portainer.DockerSnapshot{PerformanceMetrics: &portainer.PerformanceMetrics{CPUUsage: math.NaN()}}

	client := &PortainerAsyncClient{
		createSnapshotFn: func(string) (*portainer.DockerSnapshot, error) { return newDocker, nil },
	}
	client.lastSnapshot.Docker = lastDocker

	payload := &AsyncRequest{Snapshot: &snapshot{}}
	var currentSnapshot snapshot

	client.createDockerSnapshot(payload, &currentSnapshot)

	require.Same(t, newDocker, payload.Snapshot.Docker)
	require.Nil(t, payload.Snapshot.DockerPatch)
	require.Nil(t, payload.Snapshot.DockerHash)
}

func TestCreateKubernetesSnapshotGeneratesPatchAndHash(t *testing.T) {
	t.Parallel()

	lastKube := &portainer.KubernetesSnapshot{NodeCount: 1}
	newKube := &portainer.KubernetesSnapshot{NodeCount: 2}

	client := &PortainerAsyncClient{
		createKubernetesSnapshotFn: func(string) (*portainer.KubernetesSnapshot, error) { return newKube, nil },
	}
	client.lastSnapshot.Kubernetes = lastKube

	payload := &AsyncRequest{Snapshot: &snapshot{}}
	var currentSnapshot snapshot

	client.createKubernetesSnapshot(payload, &currentSnapshot)

	require.Nil(t, payload.Snapshot.Kubernetes)
	require.Same(t, newKube, currentSnapshot.Kubernetes)
	require.NotEmpty(t, payload.Snapshot.KubernetesPatch)
	require.NotNil(t, payload.Snapshot.KubernetesHash)
}

func TestCreateKubernetesSnapshotSkipsPatchWhenLastSnapshotMarshalFails(t *testing.T) {
	t.Parallel()

	lastKube := &portainer.KubernetesSnapshot{PerformanceMetrics: &portainer.PerformanceMetrics{CPUUsage: math.NaN()}}
	newKube := &portainer.KubernetesSnapshot{NodeCount: 2}

	client := &PortainerAsyncClient{
		createKubernetesSnapshotFn: func(string) (*portainer.KubernetesSnapshot, error) { return newKube, nil },
	}
	client.lastSnapshot.Kubernetes = lastKube

	payload := &AsyncRequest{Snapshot: &snapshot{}}
	var currentSnapshot snapshot

	client.createKubernetesSnapshot(payload, &currentSnapshot)

	require.Same(t, newKube, payload.Snapshot.Kubernetes)
	require.Nil(t, payload.Snapshot.KubernetesPatch)
	require.Nil(t, payload.Snapshot.KubernetesHash)
}

func TestCreateKubernetesSnapshotSkipsPatchWhenCurrentSnapshotMarshalFails(t *testing.T) {
	t.Parallel()

	lastKube := &portainer.KubernetesSnapshot{NodeCount: 1}
	newKube := &portainer.KubernetesSnapshot{PerformanceMetrics: &portainer.PerformanceMetrics{CPUUsage: math.NaN()}}

	client := &PortainerAsyncClient{
		createKubernetesSnapshotFn: func(string) (*portainer.KubernetesSnapshot, error) { return newKube, nil },
	}
	client.lastSnapshot.Kubernetes = lastKube

	payload := &AsyncRequest{Snapshot: &snapshot{}}
	var currentSnapshot snapshot

	client.createKubernetesSnapshot(payload, &currentSnapshot)

	require.Same(t, newKube, payload.Snapshot.Kubernetes)
	require.Nil(t, payload.Snapshot.KubernetesPatch)
	require.Nil(t, payload.Snapshot.KubernetesHash)
}
