package client

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"github.com/portainer/agent"
	aos "github.com/portainer/agent/os"
	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/pkg/fips"
	pkgmetrics "github.com/portainer/portainer/pkg/metrics"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetEdgeConfig(t *testing.T) {
	t.Parallel()
	fips.InitFIPS(false)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"Id": 1, "Name": "test"}`))
	}))
	defer srv.Close()

	client := &PortainerEdgeClient{
		getEndpointIDFn: func() portainer.EndpointID { return 1 },
		httpClient:      BuildHTTPClient(30, &agent.Options{}),
		serverAddress:   srv.URL,
	}

	edgeConfig, err := client.GetEdgeConfig(EdgeConfigID(1))
	require.NoError(t, err)
	require.Equal(t, edgeConfig.ID, EdgeConfigID(1))
	require.Equal(t, "test", edgeConfig.Name)
}

func TestMutateResponseForCaching(t *testing.T) {
	t.Parallel()
	// Create a test response with stacks that have ForceRedeploy set
	originalResp := PollStatusResponse{
		Stacks: []StackStatus{
			{ID: 1, Name: "stack1", ForceRedeploy: true},
			{ID: 2, Name: "stack2", ForceRedeploy: false},
			{ID: 3, Name: "stack3", ForceRedeploy: true},
		},
	}

	respForCache := mutateResponseForCaching(&originalResp)

	// Test response for cache
	require.Len(t, respForCache.Stacks, 3)
	require.Equal(t, 1, respForCache.Stacks[0].ID)
	require.Equal(t, "stack1", respForCache.Stacks[0].Name)
	// Should be mutated to false
	require.False(t, respForCache.Stacks[0].ForceRedeploy)

	require.Equal(t, 2, respForCache.Stacks[1].ID)
	require.Equal(t, "stack2", respForCache.Stacks[1].Name)
	// Should remain false
	require.False(t, respForCache.Stacks[1].ForceRedeploy)

	require.Equal(t, 3, respForCache.Stacks[2].ID)
	require.Equal(t, "stack3", respForCache.Stacks[2].Name)
	// Should be mutated to false
	require.False(t, respForCache.Stacks[2].ForceRedeploy)

	// Test that modifying the original doesn't affect the cloned responses
	require.Len(t, originalResp.Stacks, 3)
	require.Equal(t, 1, originalResp.Stacks[0].ID)
	require.Equal(t, "stack1", originalResp.Stacks[0].Name)
	require.True(t, originalResp.Stacks[0].ForceRedeploy)

	require.Equal(t, 2, originalResp.Stacks[1].ID)
	require.Equal(t, "stack2", originalResp.Stacks[1].Name)
	require.False(t, originalResp.Stacks[1].ForceRedeploy)

	require.Equal(t, 3, originalResp.Stacks[2].ID)
	require.Equal(t, "stack3", originalResp.Stacks[2].Name)
	require.True(t, originalResp.Stacks[2].ForceRedeploy)
}

func TestUpdatePolicyChartStatuses_ReturnsErrorOnServerError(t *testing.T) {
	t.Parallel()
	fips.InitFIPS(false)

	var requests atomic.Int32
	httpClient := BuildHTTPClient(30, &agent.Options{})
	httpClient.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests.Add(1)
		require.Equal(t, http.MethodPut, r.Method)
		require.Equal(t, "/api/endpoints/1/edge/charts/statuses", r.URL.Path)
		require.Equal(t, "edge-id", r.Header.Get(agent.HTTPEdgeIdentifierHeaderName))

		return &http.Response{
			StatusCode: http.StatusInternalServerError,
			Body:       io.NopCloser(strings.NewReader("")),
			Header:     make(http.Header),
			Request:    r,
		}, nil
	})

	client := &PortainerEdgeClient{
		edgeID:          "edge-id",
		getEndpointIDFn: func() portainer.EndpointID { return 1 },
		httpClient:      httpClient,
		serverAddress:   "http://edge.test",
	}

	err := client.UpdatePolicyChartStatuses([]portainer.PolicyChartStatus{{ChartName: "gatekeeper"}})
	require.Error(t, err)
	require.Equal(t, int32(1), requests.Load())
}

func TestUpdatePolicyChartStatuses_ReturnsErrorOnTransportError(t *testing.T) {
	t.Parallel()
	fips.InitFIPS(false)

	var requests atomic.Int32
	httpClient := BuildHTTPClient(30, &agent.Options{})
	httpClient.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests.Add(1)
		require.Equal(t, http.MethodPut, req.Method)
		require.Equal(t, "/api/endpoints/1/edge/charts/statuses", req.URL.Path)
		require.Equal(t, "edge-id", req.Header.Get(agent.HTTPEdgeIdentifierHeaderName))

		return nil, errors.New("dial timeout")
	})

	client := &PortainerEdgeClient{
		edgeID:          "edge-id",
		getEndpointIDFn: func() portainer.EndpointID { return 1 },
		httpClient:      httpClient,
		serverAddress:   "http://edge.test",
	}

	err := client.UpdatePolicyChartStatuses([]portainer.PolicyChartStatus{{ChartName: "gatekeeper"}})
	require.ErrorContains(t, err, "could not update policy chart statuses")
	require.Equal(t, int32(1), requests.Load())
}

func TestUpdatePolicyChartStatuses_DoesNotRetryOnClientError(t *testing.T) {
	t.Parallel()
	fips.InitFIPS(false)

	var requests atomic.Int32
	httpClient := BuildHTTPClient(30, &agent.Options{})
	httpClient.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests.Add(1)
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Body:       io.NopCloser(strings.NewReader("")),
			Header:     make(http.Header),
			Request:    r,
		}, nil
	})

	client := &PortainerEdgeClient{
		edgeID:          "edge-id",
		getEndpointIDFn: func() portainer.EndpointID { return 1 },
		httpClient:      httpClient,
		serverAddress:   "http://edge.test",
	}

	err := client.UpdatePolicyChartStatuses([]portainer.PolicyChartStatus{{ChartName: "gatekeeper"}})
	require.Error(t, err)
	require.Equal(t, int32(1), requests.Load())
}

func TestSetAlertStateCachesHeader(t *testing.T) {
	fips.InitFIPS(false)

	httpClient := BuildHTTPClient(30, &agent.Options{})
	httpClient.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		require.JSONEq(t, `{"rules":[{"rule_id":7,"state":"firing","last_evaluation":123}]}`, r.Header.Get(agent.HTTPAlertStateHeaderName))

		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"status":"ok"}`)),
			Header:     make(http.Header),
			Request:    r,
		}, nil
	})

	client := &PortainerEdgeClient{
		edgeID:          "edge-id",
		getEndpointIDFn: func() portainer.EndpointID { return 1 },
		httpClient:      httpClient,
		serverAddress:   "http://edge.test",
	}

	client.SetAlertState(&pkgmetrics.EdgeAlertState{
		Rules: []pkgmetrics.EdgeAlertRuleState{{
			RuleID:         7,
			State:          pkgmetrics.AlertRuleStateFiring,
			LastEvaluation: 123,
		}},
	})

	require.JSONEq(t, `{"rules":[{"rule_id":7,"state":"firing","last_evaluation":123}]}`, client.alertStateHeader)

	_, err := client.GetEnvironmentStatus()
	require.NoError(t, err)

	client.SetAlertState(nil)
	require.Empty(t, client.alertStateHeader)
}

func TestGetEnvironmentStatusSendsContainerEngineHeaderFromRuntimePlatform(t *testing.T) {
	fips.InitFIPS(false)
	t.Setenv(aos.PodmanMode, "1")

	httpClient := BuildHTTPClient(30, &agent.Options{})
	httpClient.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, "podman", r.Header.Get(agent.HTTPResponseAgentContainerEngine))

		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"status":"ok"}`)),
			Header:     make(http.Header),
			Request:    r,
		}, nil
	})

	client := &PortainerEdgeClient{
		edgeID:          "edge-id",
		getEndpointIDFn: func() portainer.EndpointID { return 1 },
		httpClient:      httpClient,
		serverAddress:   "http://edge.test",
		agentPlatform:   agent.PlatformDocker,
	}

	_, err := client.GetEnvironmentStatus()
	require.NoError(t, err)
}

// TestReportPolicyStatuses_SuccessOn204 verifies that a 204 response results in
// no error and exactly one request is sent (no retry logic on this path).
func TestReportPolicyStatuses_SuccessOn204(t *testing.T) {
	t.Parallel()
	fips.InitFIPS(false)

	var requests atomic.Int32
	httpClient := BuildHTTPClient(30, &agent.Options{})
	httpClient.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests.Add(1)
		require.Equal(t, http.MethodPut, r.Method)
		require.Equal(t, "/api/endpoints/1/edge/policies/statuses", r.URL.Path)
		require.Equal(t, "edge-id", r.Header.Get(agent.HTTPEdgeIdentifierHeaderName))
		return &http.Response{
			StatusCode: http.StatusNoContent,
			Body:       io.NopCloser(strings.NewReader("")),
			Header:     make(http.Header),
			Request:    r,
		}, nil
	})

	c := &PortainerEdgeClient{
		edgeID:          "edge-id",
		getEndpointIDFn: func() portainer.EndpointID { return 1 },
		httpClient:      httpClient,
		serverAddress:   "http://edge.test",
	}

	err := c.ReportPolicyStatuses([]portainer.PolicyActualState{
		{PolicyID: 1, Type: "helm-k8s", Fingerprint: "fp", Status: "applied"},
	})
	require.NoError(t, err)
	assert.Equal(t, int32(1), requests.Load(), "exactly one request must be sent")
}

// TestReportPolicyStatuses_DoesNotRetryOnNon204 verifies that a non-204 response
// (e.g. 404 from a pre-Phase-3 server) returns nil — the method logs the failure
// at Debug level and relies on the Phase-3 dual-emit legacy fallback for coverage.
// This distinguishes it from UpdatePolicyChartStatuses which retries on server errors.
func TestReportPolicyStatuses_DoesNotRetryOnNon204(t *testing.T) {
	t.Parallel()
	fips.InitFIPS(false)

	var requests atomic.Int32
	httpClient := BuildHTTPClient(30, &agent.Options{})
	httpClient.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests.Add(1)
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(strings.NewReader("")),
			Header:     make(http.Header),
			Request:    r,
		}, nil
	})

	c := &PortainerEdgeClient{
		edgeID:          "edge-id",
		getEndpointIDFn: func() portainer.EndpointID { return 1 },
		httpClient:      httpClient,
		serverAddress:   "http://edge.test",
	}

	err := c.ReportPolicyStatuses(nil)
	require.NoError(t, err, "non-204 must not return an error (logged at Debug only)")
	assert.Equal(t, int32(1), requests.Load(), "must not retry on non-204")
}

// TestReportPolicyStatuses_ReturnsErrorOnTransportFailure verifies that a transport-
// level error (e.g. connection refused) is returned as an error to the caller.
func TestReportPolicyStatuses_ReturnsErrorOnTransportFailure(t *testing.T) {
	t.Parallel()
	fips.InitFIPS(false)

	httpClient := BuildHTTPClient(30, &agent.Options{})
	httpClient.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return nil, errors.New("dial timeout")
	})

	c := &PortainerEdgeClient{
		edgeID:          "edge-id",
		getEndpointIDFn: func() portainer.EndpointID { return 1 },
		httpClient:      httpClient,
		serverAddress:   "http://edge.test",
	}

	err := c.ReportPolicyStatuses(nil)
	require.Error(t, err)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
