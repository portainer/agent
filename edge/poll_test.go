package edge

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/portainer/agent"
	"github.com/portainer/agent/edge/client"
	"github.com/portainer/agent/edge/evaluator"
	"github.com/portainer/agent/edge/stack"
	agentmetrics "github.com/portainer/agent/http/handler/metrics"
	"github.com/portainer/agent/internals/mocks"
	"github.com/portainer/agent/kubernetes"
	"github.com/portainer/agent/policyreconcile"
	portainer "github.com/portainer/portainer/api"
	pkgmetrics "github.com/portainer/portainer/pkg/metrics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestBuildMetricsScrapeTargetUsesConfiguredHostPort(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "http://127.0.0.1:9001/api/metrics", buildMetricsScrapeTarget("127.0.0.1:9001"))
}

func TestBuildMetricsScrapeTargetNormalizesUnspecifiedHost(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "http://localhost:9001/api/metrics", buildMetricsScrapeTarget(":9001"))
	assert.Equal(t, "http://localhost:9001/api/metrics", buildMetricsScrapeTarget("0.0.0.0:9001"))
	assert.Equal(t, "http://localhost:9001/api/metrics", buildMetricsScrapeTarget("[::]:9001"))
}

func TestComputeRulesHashSameContentSameHash(t *testing.T) {
	t.Parallel()
	yaml := `groups:
  - name: portainer-edge-cluster-alerts
    rules:
      - alert: High CPU
        expr: vector(1)
`
	hashA := computeRulesHash(yaml)
	hashB := computeRulesHash(yaml)
	assert.Equal(t, hashA, hashB)
}

func TestComputeRulesHashDifferentContentDifferentHash(t *testing.T) {
	t.Parallel()
	yamlA := `groups:
  - name: portainer-edge-cluster-alerts
    rules:
      - alert: High CPU
        expr: vector(1)
`
	yamlB := `groups:
  - name: portainer-edge-cluster-alerts
    rules:
      - alert: High CPU
        expr: vector(2)
`
	assert.NotEqual(t, computeRulesHash(yamlA), computeRulesHash(yamlB))
}

func TestComputeRulesHashEmptyStringIsStable(t *testing.T) {
	t.Parallel()
	hashA := computeRulesHash("")
	hashB := computeRulesHash("")
	assert.Equal(t, hashA, hashB)
}

func TestBuildEdgeAlertStateSortsRules(t *testing.T) {
	t.Parallel()
	state := buildEdgeAlertState([]pkgmetrics.EdgeAlertRuleState{
		{RuleID: 9, State: pkgmetrics.AlertRuleStatePending},
		{RuleID: 3, State: pkgmetrics.AlertRuleStateFiring},
	}, "")

	if assert.NotNil(t, state) {
		assert.Equal(t, 3, state.Rules[0].RuleID)
		assert.Equal(t, 9, state.Rules[1].RuleID)
	}
}

func TestBuildEdgeAlertStateReturnsNilWhenEmpty(t *testing.T) {
	t.Parallel()
	assert.Nil(t, buildEdgeAlertState(nil, ""))
}

func TestPollPublishesAlertStateAfterReloadHandling(t *testing.T) {
	ctrl := gomock.NewController(t)

	dataDir := t.TempDir()
	manager := NewManager(&ManagerParameters{
		Options: &agent.Options{DataPath: dataDir},
	})
	manager.key = &edgeKey{EndpointID: 1, Global: true}

	pollResponse := &client.PollStatusResponse{
		AlertRulesYAML: `groups:
  - name: portainer-edge-cluster-alerts
    rules:
      - alert: BrokenRule
`,
	}

	mockClient := mocks.NewMockPortainerClient(ctrl)
	mockClient.EXPECT().GetEnvironmentStatus().Return(pollResponse, nil)

	var capturedAlertState *pkgmetrics.EdgeAlertState
	mockClient.EXPECT().SetAlertState(gomock.Any()).Do(func(state *pkgmetrics.EdgeAlertState) {
		capturedAlertState = state
	})

	mockScheduler := mocks.NewMockScheduler(ctrl)
	mockScheduler.EXPECT().Schedule(gomock.Any()).Return(nil)

	eval, err := evaluator.New(evaluator.Config{
		DataDir:    filepath.Join(dataDir, "tsdb"),
		EndpointID: 1,
	})
	require.NoError(t, err)
	eval.Start()
	t.Cleanup(eval.Stop)

	service := &PollService{
		edgeManager:      manager,
		edgeStackManager: stack.NewStackManager(mockClient, nil, "edge-id", nil),
		firstPoll:        false,
		portainerClient:  mockClient,
		scheduleManager:  mockScheduler,
		evaluator:        eval,
	}

	err = service.poll()
	require.NoError(t, err)
	require.NotNil(t, capturedAlertState)
	assert.Contains(t, capturedAlertState.ConfigReloadError, "invalid alert rules YAML from server")
}

func TestPushPerformanceMetricsClearsSnapshotOnCollectionFailure(t *testing.T) {
	oldCollectRawMetricsFn := collectRawMetricsFn
	oldCollectNodeConditionsFn := collectNodeConditionsFn
	oldCollectEtcdHealthFn := collectEtcdHealthFn
	oldCollectAPIServerCertFn := collectAPIServerCertFn
	oldCollectAPIServerHealthFn := collectAPIServerHealthFn
	t.Cleanup(func() {
		collectRawMetricsFn = oldCollectRawMetricsFn
		collectNodeConditionsFn = oldCollectNodeConditionsFn
		collectEtcdHealthFn = oldCollectEtcdHealthFn
		collectAPIServerCertFn = oldCollectAPIServerCertFn
		collectAPIServerHealthFn = oldCollectAPIServerHealthFn
	})

	manager := NewManager(&ManagerParameters{
		Options:           &agent.Options{DataPath: t.TempDir()},
		ContainerPlatform: agent.PlatformKubernetes,
	})
	service := &PollService{
		edgeManager:    manager,
		metricsHandler: manager.MetricsHandler(),
	}

	collectNodeConditionsFn = func(_ context.Context, _ *kubernetes.KubeClient) ([]kubernetes.NodeReadyStatus, error) {
		return nil, errors.New("collect node conditions failed")
	}
	collectEtcdHealthFn = func(_ context.Context, _ *kubernetes.KubeClient) (bool, error) {
		return false, nil
	}

	collectRawMetricsFn = func(_ context.Context, _ *kubernetes.KubeClient) (*kubernetes.ClusterRawMetrics, error) {
		return &kubernetes.ClusterRawMetrics{
			HasCPU:               true,
			CPUUsageNanoCores:    2_000_000_000,
			CPUCapacityNanoCores: 4_000_000_000,
		}, nil
	}
	collectAPIServerCertFn = func(_ context.Context, _ *kubernetes.KubeClient) (*kubernetes.TLSCertInfo, error) {
		return nil, errors.New("collect tls cert failed")
	}
	collectAPIServerHealthFn = func(_ context.Context, _ *kubernetes.KubeClient) bool {
		return true
	}
	service.pushPerformanceMetrics(context.Background())
	require.Contains(t, serveMetrics(t, service.metricsHandler), pkgmetrics.ClusterCPUUsageCoresMetric)

	collectRawMetricsFn = func(_ context.Context, _ *kubernetes.KubeClient) (*kubernetes.ClusterRawMetrics, error) {
		return nil, errors.New("collect failed")
	}
	service.pushPerformanceMetrics(context.Background())

	body := serveMetrics(t, service.metricsHandler)
	require.NotContains(t, body, pkgmetrics.ClusterCPUUsageCoresMetric)
	require.Contains(t, body, pkgmetrics.ClusterEtcdHealthyMetric+" 0")
	require.Contains(t, body, pkgmetrics.ClusterEtcdHealthValidMetric+" 1")
}

func TestPushPerformanceMetricsUpdatesNodeReadinessWhenRawCollectionFails(t *testing.T) {
	oldCollectRawMetricsFn := collectRawMetricsFn
	oldCollectNodeConditionsFn := collectNodeConditionsFn
	oldCollectEtcdHealthFn := collectEtcdHealthFn
	oldCollectAPIServerCertFn := collectAPIServerCertFn
	oldCollectAPIServerHealthFn := collectAPIServerHealthFn
	t.Cleanup(func() {
		collectRawMetricsFn = oldCollectRawMetricsFn
		collectNodeConditionsFn = oldCollectNodeConditionsFn
		collectEtcdHealthFn = oldCollectEtcdHealthFn
		collectAPIServerCertFn = oldCollectAPIServerCertFn
		collectAPIServerHealthFn = oldCollectAPIServerHealthFn
	})

	manager := NewManager(&ManagerParameters{
		Options:           &agent.Options{DataPath: t.TempDir()},
		ContainerPlatform: agent.PlatformKubernetes,
	})
	service := &PollService{
		edgeManager:    manager,
		metricsHandler: manager.MetricsHandler(),
	}

	collectRawMetricsFn = func(_ context.Context, _ *kubernetes.KubeClient) (*kubernetes.ClusterRawMetrics, error) {
		return nil, errors.New("collect raw metrics failed")
	}
	collectNodeConditionsFn = func(_ context.Context, _ *kubernetes.KubeClient) ([]kubernetes.NodeReadyStatus, error) {
		return []kubernetes.NodeReadyStatus{{Name: "node-a", Ready: false}}, nil
	}
	collectEtcdHealthFn = func(_ context.Context, _ *kubernetes.KubeClient) (bool, error) {
		return true, nil
	}
	collectAPIServerCertFn = func(_ context.Context, _ *kubernetes.KubeClient) (*kubernetes.TLSCertInfo, error) {
		return nil, errors.New("collect tls cert failed")
	}
	collectAPIServerHealthFn = func(_ context.Context, _ *kubernetes.KubeClient) bool {
		return true
	}

	service.pushPerformanceMetrics(context.Background())

	body := serveMetrics(t, service.metricsHandler)
	require.NotContains(t, body, pkgmetrics.ClusterCPUUsageCoresMetric)
	require.Contains(t, body, pkgmetrics.ClusterNodeReadyMetric+"{node=\"node-a\"} 0")
	require.Contains(t, body, pkgmetrics.ClusterEtcdHealthyMetric+" 1")
	require.Contains(t, body, pkgmetrics.ClusterEtcdHealthValidMetric+" 1")
}

func TestPushPerformanceMetricsClearsNodeReadinessOnNodeCollectionFailure(t *testing.T) {
	oldCollectRawMetricsFn := collectRawMetricsFn
	oldCollectNodeConditionsFn := collectNodeConditionsFn
	oldCollectEtcdHealthFn := collectEtcdHealthFn
	oldCollectAPIServerCertFn := collectAPIServerCertFn
	oldCollectAPIServerHealthFn := collectAPIServerHealthFn
	t.Cleanup(func() {
		collectRawMetricsFn = oldCollectRawMetricsFn
		collectNodeConditionsFn = oldCollectNodeConditionsFn
		collectEtcdHealthFn = oldCollectEtcdHealthFn
		collectAPIServerCertFn = oldCollectAPIServerCertFn
		collectAPIServerHealthFn = oldCollectAPIServerHealthFn
	})

	manager := NewManager(&ManagerParameters{
		Options:           &agent.Options{DataPath: t.TempDir()},
		ContainerPlatform: agent.PlatformKubernetes,
	})
	service := &PollService{
		edgeManager:    manager,
		metricsHandler: manager.MetricsHandler(),
	}

	collectRawMetricsFn = func(_ context.Context, _ *kubernetes.KubeClient) (*kubernetes.ClusterRawMetrics, error) {
		return &kubernetes.ClusterRawMetrics{
			HasCPU:               true,
			CPUUsageNanoCores:    1_000_000_000,
			CPUCapacityNanoCores: 2_000_000_000,
		}, nil
	}
	collectNodeConditionsFn = func(_ context.Context, _ *kubernetes.KubeClient) ([]kubernetes.NodeReadyStatus, error) {
		return []kubernetes.NodeReadyStatus{{Name: "node-a", Ready: true}}, nil
	}
	collectEtcdHealthFn = func(_ context.Context, _ *kubernetes.KubeClient) (bool, error) {
		return true, nil
	}
	collectAPIServerCertFn = func(_ context.Context, _ *kubernetes.KubeClient) (*kubernetes.TLSCertInfo, error) {
		return nil, errors.New("collect tls cert failed")
	}
	collectAPIServerHealthFn = func(_ context.Context, _ *kubernetes.KubeClient) bool {
		return true
	}

	service.pushPerformanceMetrics(context.Background())
	require.Contains(t, serveMetrics(t, service.metricsHandler), pkgmetrics.ClusterNodeReadyMetric+"{node=\"node-a\"} 1")

	collectNodeConditionsFn = func(_ context.Context, _ *kubernetes.KubeClient) ([]kubernetes.NodeReadyStatus, error) {
		return nil, errors.New("collect node conditions failed")
	}

	service.pushPerformanceMetrics(context.Background())
	body := serveMetrics(t, service.metricsHandler)
	require.Contains(t, body, pkgmetrics.ClusterCPUUsageCoresMetric)
	require.NotContains(t, body, pkgmetrics.ClusterNodeReadyMetric)
	require.Contains(t, body, pkgmetrics.ClusterEtcdHealthyMetric+" 1")
	require.Contains(t, body, pkgmetrics.ClusterEtcdHealthValidMetric+" 1")
}

func TestPushPerformanceMetricsSkipsEtcdUpdateOnCollectionFailure(t *testing.T) {
	oldCollectRawMetricsFn := collectRawMetricsFn
	oldCollectNodeConditionsFn := collectNodeConditionsFn
	oldCollectEtcdHealthFn := collectEtcdHealthFn
	oldCollectAPIServerHealthFn := collectAPIServerHealthFn
	t.Cleanup(func() {
		collectRawMetricsFn = oldCollectRawMetricsFn
		collectNodeConditionsFn = oldCollectNodeConditionsFn
		collectEtcdHealthFn = oldCollectEtcdHealthFn
		collectAPIServerHealthFn = oldCollectAPIServerHealthFn
	})

	manager := NewManager(&ManagerParameters{
		Options:           &agent.Options{DataPath: t.TempDir()},
		ContainerPlatform: agent.PlatformKubernetes,
	})
	service := &PollService{
		edgeManager:    manager,
		metricsHandler: manager.MetricsHandler(),
	}

	collectRawMetricsFn = func(_ context.Context, _ *kubernetes.KubeClient) (*kubernetes.ClusterRawMetrics, error) {
		return &kubernetes.ClusterRawMetrics{
			HasCPU:               true,
			CPUUsageNanoCores:    1_000_000_000,
			CPUCapacityNanoCores: 2_000_000_000,
		}, nil
	}
	collectNodeConditionsFn = func(_ context.Context, _ *kubernetes.KubeClient) ([]kubernetes.NodeReadyStatus, error) {
		return []kubernetes.NodeReadyStatus{}, nil
	}
	collectEtcdHealthFn = func(_ context.Context, _ *kubernetes.KubeClient) (bool, error) {
		return true, nil
	}
	collectAPIServerHealthFn = func(_ context.Context, _ *kubernetes.KubeClient) bool {
		return true
	}

	service.pushPerformanceMetrics(context.Background())
	body := serveMetrics(t, service.metricsHandler)
	require.Contains(t, body, pkgmetrics.ClusterEtcdHealthyMetric+" 1")
	require.Contains(t, body, pkgmetrics.ClusterEtcdHealthValidMetric+" 1")

	collectEtcdHealthFn = func(_ context.Context, _ *kubernetes.KubeClient) (bool, error) {
		return false, errors.New("collect etcd health failed")
	}

	service.pushPerformanceMetrics(context.Background())
	body = serveMetrics(t, service.metricsHandler)
	require.Contains(t, body, pkgmetrics.ClusterCPUUsageCoresMetric)
	require.Contains(t, body, pkgmetrics.ClusterEtcdHealthyMetric+" 1")
	require.Contains(t, body, pkgmetrics.ClusterEtcdHealthValidMetric+" 0")
}

// TestVersionSkew_NewAgentOldServer verifies that when an older server sends a
// response with no PolicyStates, the new agent falls back to the legacy
// PolicyChartSummaries path without error.
func TestVersionSkew_NewAgentOldServer(t *testing.T) {
	t.Parallel()
	// Old server: response has PolicyChartSummaries but no PolicyStates.
	oldServerResponse := &client.PollStatusResponse{
		PolicyChartSummaries: []portainer.PolicyChartSummary{
			{ChartName: "gatekeeper", Fingerprint: "fp1"},
		},
	}
	assert.False(t, hasPerPolicyPayload(oldServerResponse),
		"old server response (no PolicyStates) must not trigger per-policy path")
}

// TestVersionSkew_NewAgentNewServer verifies that when a new server sends
// PolicyStates the new agent routes through the per-policy path.
func TestVersionSkew_NewAgentNewServer(t *testing.T) {
	t.Parallel()
	states := []portainer.PolicyDesiredState{
		{PolicyID: 42, Type: "helm-k8s", Fingerprint: "fp1"},
	}
	newServerResponse := &client.PollStatusResponse{PolicyStates: &states}
	assert.True(t, hasPerPolicyPayload(newServerResponse),
		"new server response (with PolicyStates) must trigger per-policy path")
}

// TestVersionSkew_NewServerNoPolicies verifies that a new server with zero active
// policies still triggers the per-policy path (empty desired list → remove all
// active handlers). This is the key regression this fix addresses.
func TestVersionSkew_NewServerNoPolicies(t *testing.T) {
	t.Parallel()
	empty := []portainer.PolicyDesiredState{}
	response := &client.PollStatusResponse{PolicyStates: &empty}
	assert.True(t, hasPerPolicyPayload(response),
		"new server with no policies must still trigger reconcilePolicies([]) to remove active handlers")
}

// TestVersionSkew_EmptyResponse verifies that a response with neither field
// (e.g. unparseable agent version causes PayloadVariantNone on the server)
// does not trigger the per-policy path.
func TestVersionSkew_EmptyResponse(t *testing.T) {
	t.Parallel()
	empty := &client.PollStatusResponse{}
	assert.False(t, hasPerPolicyPayload(empty), "nil PolicyStates must use legacy path")
}

func TestEnqueuePolicyReconcileKeepsLatestPendingPayload(t *testing.T) {
	t.Parallel()
	service := &PollService{
		policyReconcileCh: make(chan []portainer.PolicyDesiredState, 1),
	}

	first := []portainer.PolicyDesiredState{{PolicyID: 1, Fingerprint: "old"}}
	second := []portainer.PolicyDesiredState{{PolicyID: 2, Fingerprint: "new"}}

	service.enqueuePolicyReconcile(first)
	service.enqueuePolicyReconcile(second)

	queued := <-service.policyReconcileCh
	require.Len(t, queued, 1)
	assert.Equal(t, portainer.PolicyID(2), queued[0].PolicyID)
	assert.Equal(t, "new", queued[0].Fingerprint)
}

func TestToPolicyActualStates_FieldMapping(t *testing.T) {
	t.Parallel()
	in := []policyreconcile.ActualState{
		{PolicyID: 1, Type: "helm-k8s", Fingerprint: "fp1", Status: policyreconcile.StatusApplied, Message: ""},
		{PolicyID: 2, Type: "helm-k8s", Fingerprint: "", Status: policyreconcile.StatusFailed, Message: "install failed"},
		{PolicyID: 3, Type: "helm-k8s", Fingerprint: "fp3", Status: policyreconcile.StatusApplying, Message: "in progress"},
	}

	out := toPolicyActualStates(in)

	require.Len(t, out, 3)
	assert.Equal(t, portainer.PolicyID(1), out[0].PolicyID)
	assert.Equal(t, "helm-k8s", out[0].Type)
	assert.Equal(t, "fp1", out[0].Fingerprint)
	assert.Equal(t, "applied", out[0].Status)
	assert.Empty(t, out[0].Message)

	assert.Equal(t, portainer.PolicyID(2), out[1].PolicyID)
	assert.Equal(t, "failed", out[1].Status)
	assert.Equal(t, "install failed", out[1].Message)
	assert.Empty(t, out[1].Fingerprint)

	assert.Equal(t, "applying", out[2].Status)
}

func TestToPolicyActualStates_EmptyInput(t *testing.T) {
	t.Parallel()
	assert.Empty(t, toPolicyActualStates(nil))
	assert.Empty(t, toPolicyActualStates([]policyreconcile.ActualState{}))
}

func TestToDesiredStates_FieldMapping(t *testing.T) {
	t.Parallel()
	cfg := []byte(`{"charts":[]}`)
	in := []portainer.PolicyDesiredState{
		{PolicyID: 42, Type: "helm-k8s", Fingerprint: "abc", Config: cfg},
		{PolicyID: 99, Type: "helm-k8s", Fingerprint: "", Config: nil},
	}

	out := toDesiredStates(in)

	require.Len(t, out, 2)
	assert.Equal(t, portainer.PolicyID(42), out[0].PolicyID)
	assert.Equal(t, "helm-k8s", out[0].Type)
	assert.Equal(t, "abc", out[0].Fingerprint)
	assert.Equal(t, policyreconcile.DesiredState{}.Config, out[1].Config)
	assert.EqualValues(t, cfg, out[0].Config)
}

func TestToDesiredStates_EmptyInput(t *testing.T) {
	t.Parallel()
	assert.Empty(t, toDesiredStates(nil))
}

func TestDesiredStateIDs_ExtractsAllIDs(t *testing.T) {
	t.Parallel()
	states := []policyreconcile.DesiredState{
		{PolicyID: 10},
		{PolicyID: 20},
		{PolicyID: 30},
	}
	ids := desiredStateIDs(states)
	require.Len(t, ids, 3)
	assert.Equal(t, portainer.PolicyID(10), ids[0])
	assert.Equal(t, portainer.PolicyID(20), ids[1])
	assert.Equal(t, portainer.PolicyID(30), ids[2])
}

func TestDesiredStateIDs_EmptyInput(t *testing.T) {
	t.Parallel()
	assert.Empty(t, desiredStateIDs(nil))
}

func TestPushPerformanceMetricsUpdatesTLSCertGaugeOnSuccess(t *testing.T) {
	oldCollectRawMetricsFn := collectRawMetricsFn
	oldCollectNodeConditionsFn := collectNodeConditionsFn
	oldCollectEtcdHealthFn := collectEtcdHealthFn
	oldCollectAPIServerCertFn := collectAPIServerCertFn
	oldCollectAPIServerHealthFn := collectAPIServerHealthFn
	t.Cleanup(func() {
		collectRawMetricsFn = oldCollectRawMetricsFn
		collectNodeConditionsFn = oldCollectNodeConditionsFn
		collectEtcdHealthFn = oldCollectEtcdHealthFn
		collectAPIServerCertFn = oldCollectAPIServerCertFn
		collectAPIServerHealthFn = oldCollectAPIServerHealthFn
	})

	manager := NewManager(&ManagerParameters{
		Options:           &agent.Options{DataPath: t.TempDir()},
		ContainerPlatform: agent.PlatformKubernetes,
	})
	service := &PollService{
		edgeManager:    manager,
		metricsHandler: manager.MetricsHandler(),
	}

	collectRawMetricsFn = func(_ context.Context, _ *kubernetes.KubeClient) (*kubernetes.ClusterRawMetrics, error) {
		return nil, errors.New("collect raw metrics failed")
	}
	collectNodeConditionsFn = func(_ context.Context, _ *kubernetes.KubeClient) ([]kubernetes.NodeReadyStatus, error) {
		return nil, errors.New("collect node conditions failed")
	}
	collectEtcdHealthFn = func(_ context.Context, _ *kubernetes.KubeClient) (bool, error) {
		return false, errors.New("collect etcd health failed")
	}
	collectAPIServerCertFn = func(_ context.Context, _ *kubernetes.KubeClient) (*kubernetes.TLSCertInfo, error) {
		return &kubernetes.TLSCertInfo{Source: "apiserver", CN: "kube-apiserver", NotAfter: time.Unix(1_900_000_000, 0)}, nil
	}
	collectAPIServerHealthFn = func(_ context.Context, _ *kubernetes.KubeClient) bool {
		return true
	}

	service.pushPerformanceMetrics(context.Background())
	body := serveMetrics(t, service.metricsHandler)
	require.Contains(t, body, pkgmetrics.ClusterAPIServerTLSCertExpirySecondsMetric)
	require.Contains(t, body, "cn=\"kube-apiserver\"")
	require.Contains(t, body, "source=\"apiserver\"")
}

func TestPushPerformanceMetricsClearsTLSCertGaugeOnCollectionFailure(t *testing.T) {
	oldCollectRawMetricsFn := collectRawMetricsFn
	oldCollectNodeConditionsFn := collectNodeConditionsFn
	oldCollectEtcdHealthFn := collectEtcdHealthFn
	oldCollectAPIServerCertFn := collectAPIServerCertFn
	oldCollectAPIServerHealthFn := collectAPIServerHealthFn
	t.Cleanup(func() {
		collectRawMetricsFn = oldCollectRawMetricsFn
		collectNodeConditionsFn = oldCollectNodeConditionsFn
		collectEtcdHealthFn = oldCollectEtcdHealthFn
		collectAPIServerCertFn = oldCollectAPIServerCertFn
		collectAPIServerHealthFn = oldCollectAPIServerHealthFn
	})

	manager := NewManager(&ManagerParameters{
		Options:           &agent.Options{DataPath: t.TempDir()},
		ContainerPlatform: agent.PlatformKubernetes,
	})
	service := &PollService{
		edgeManager:    manager,
		metricsHandler: manager.MetricsHandler(),
	}

	collectRawMetricsFn = func(_ context.Context, _ *kubernetes.KubeClient) (*kubernetes.ClusterRawMetrics, error) {
		return nil, errors.New("collect raw metrics failed")
	}
	collectNodeConditionsFn = func(_ context.Context, _ *kubernetes.KubeClient) ([]kubernetes.NodeReadyStatus, error) {
		return nil, errors.New("collect node conditions failed")
	}
	collectEtcdHealthFn = func(_ context.Context, _ *kubernetes.KubeClient) (bool, error) {
		return false, errors.New("collect etcd health failed")
	}
	collectAPIServerCertFn = func(_ context.Context, _ *kubernetes.KubeClient) (*kubernetes.TLSCertInfo, error) {
		return &kubernetes.TLSCertInfo{Source: "apiserver", CN: "kube-apiserver", NotAfter: time.Unix(1_900_000_000, 0)}, nil
	}
	collectAPIServerHealthFn = func(_ context.Context, _ *kubernetes.KubeClient) bool {
		return true
	}

	service.pushPerformanceMetrics(context.Background())
	require.Contains(t, serveMetrics(t, service.metricsHandler), pkgmetrics.ClusterAPIServerTLSCertExpirySecondsMetric)

	collectAPIServerCertFn = func(_ context.Context, _ *kubernetes.KubeClient) (*kubernetes.TLSCertInfo, error) {
		return nil, errors.New("collect tls cert failed")
	}

	service.pushPerformanceMetrics(context.Background())
	body := serveMetrics(t, service.metricsHandler)
	require.NotContains(t, body, pkgmetrics.ClusterAPIServerTLSCertExpirySecondsMetric)
}

func TestPushPerformanceMetricsUpdatesAPIServerHealthGaugeOnSuccess(t *testing.T) {
	oldCollectRawMetricsFn := collectRawMetricsFn
	oldCollectNodeConditionsFn := collectNodeConditionsFn
	oldCollectEtcdHealthFn := collectEtcdHealthFn
	oldCollectAPIServerCertFn := collectAPIServerCertFn
	oldCollectAPIServerHealthFn := collectAPIServerHealthFn
	t.Cleanup(func() {
		collectRawMetricsFn = oldCollectRawMetricsFn
		collectNodeConditionsFn = oldCollectNodeConditionsFn
		collectEtcdHealthFn = oldCollectEtcdHealthFn
		collectAPIServerCertFn = oldCollectAPIServerCertFn
		collectAPIServerHealthFn = oldCollectAPIServerHealthFn
	})

	manager := NewManager(&ManagerParameters{
		Options:           &agent.Options{DataPath: t.TempDir()},
		ContainerPlatform: agent.PlatformKubernetes,
	})
	service := &PollService{
		edgeManager:    manager,
		metricsHandler: manager.MetricsHandler(),
	}

	collectRawMetricsFn = func(_ context.Context, _ *kubernetes.KubeClient) (*kubernetes.ClusterRawMetrics, error) {
		return nil, errors.New("collect raw metrics failed")
	}
	collectNodeConditionsFn = func(_ context.Context, _ *kubernetes.KubeClient) ([]kubernetes.NodeReadyStatus, error) {
		return nil, errors.New("collect node conditions failed")
	}
	collectEtcdHealthFn = func(_ context.Context, _ *kubernetes.KubeClient) (bool, error) {
		return false, errors.New("collect etcd health failed")
	}
	collectAPIServerCertFn = func(_ context.Context, _ *kubernetes.KubeClient) (*kubernetes.TLSCertInfo, error) {
		return nil, errors.New("collect tls cert failed")
	}
	collectAPIServerHealthFn = func(_ context.Context, _ *kubernetes.KubeClient) bool {
		return false
	}

	service.pushPerformanceMetrics(context.Background())

	body := serveMetrics(t, service.metricsHandler)
	require.Contains(t, body, pkgmetrics.ClusterAPIServerHealthyMetric+" 0")
}

func TestPushPerformanceMetricsTransitionsAPIServerHealthGauge(t *testing.T) {
	oldCollectRawMetricsFn := collectRawMetricsFn
	oldCollectNodeConditionsFn := collectNodeConditionsFn
	oldCollectEtcdHealthFn := collectEtcdHealthFn
	oldCollectAPIServerCertFn := collectAPIServerCertFn
	oldCollectAPIServerHealthFn := collectAPIServerHealthFn
	oldCollectAPIServerLatencyFn := collectAPIServerLatencyFn
	t.Cleanup(func() {
		collectRawMetricsFn = oldCollectRawMetricsFn
		collectNodeConditionsFn = oldCollectNodeConditionsFn
		collectEtcdHealthFn = oldCollectEtcdHealthFn
		collectAPIServerCertFn = oldCollectAPIServerCertFn
		collectAPIServerHealthFn = oldCollectAPIServerHealthFn
		collectAPIServerLatencyFn = oldCollectAPIServerLatencyFn
	})

	manager := NewManager(&ManagerParameters{
		Options:           &agent.Options{DataPath: t.TempDir()},
		ContainerPlatform: agent.PlatformKubernetes,
	})
	service := &PollService{
		edgeManager:    manager,
		metricsHandler: manager.MetricsHandler(),
	}

	collectRawMetricsFn = func(_ context.Context, _ *kubernetes.KubeClient) (*kubernetes.ClusterRawMetrics, error) {
		return nil, errors.New("collect raw metrics failed")
	}
	collectNodeConditionsFn = func(_ context.Context, _ *kubernetes.KubeClient) ([]kubernetes.NodeReadyStatus, error) {
		return nil, errors.New("collect node conditions failed")
	}
	collectEtcdHealthFn = func(_ context.Context, _ *kubernetes.KubeClient) (bool, error) {
		return false, errors.New("collect etcd health failed")
	}
	collectAPIServerCertFn = func(_ context.Context, _ *kubernetes.KubeClient) (*kubernetes.TLSCertInfo, error) {
		return nil, errors.New("collect tls cert failed")
	}
	collectAPIServerHealthFn = func(_ context.Context, _ *kubernetes.KubeClient) bool {
		return true
	}
	collectAPIServerLatencyFn = func(_ context.Context, _ *kubernetes.KubeClient) (kubernetes.APIServerLatencyHistogram, error) {
		return kubernetes.APIServerLatencyHistogram{}, kubernetes.ErrAPIServerRequestLatencyUnsupported
	}

	service.pushPerformanceMetrics(context.Background())
	require.Contains(t, serveMetrics(t, service.metricsHandler), pkgmetrics.ClusterAPIServerHealthyMetric+" 1")

	collectAPIServerHealthFn = func(_ context.Context, _ *kubernetes.KubeClient) bool {
		return false
	}

	service.pushPerformanceMetrics(context.Background())
	body := serveMetrics(t, service.metricsHandler)
	require.Contains(t, body, pkgmetrics.ClusterAPIServerHealthyMetric+" 0")
}

func TestPushPerformanceMetricsUpdatesAPIServerLatencyOnSuccess(t *testing.T) {
	oldCollectRawMetricsFn := collectRawMetricsFn
	oldCollectNodeConditionsFn := collectNodeConditionsFn
	oldCollectEtcdHealthFn := collectEtcdHealthFn
	oldCollectAPIServerCertFn := collectAPIServerCertFn
	oldCollectAPIServerHealthFn := collectAPIServerHealthFn
	oldCollectAPIServerLatencyFn := collectAPIServerLatencyFn
	t.Cleanup(func() {
		collectRawMetricsFn = oldCollectRawMetricsFn
		collectNodeConditionsFn = oldCollectNodeConditionsFn
		collectEtcdHealthFn = oldCollectEtcdHealthFn
		collectAPIServerCertFn = oldCollectAPIServerCertFn
		collectAPIServerHealthFn = oldCollectAPIServerHealthFn
		collectAPIServerLatencyFn = oldCollectAPIServerLatencyFn
	})

	manager := NewManager(&ManagerParameters{
		Options:           &agent.Options{DataPath: t.TempDir()},
		ContainerPlatform: agent.PlatformKubernetes,
	})
	service := &PollService{
		edgeManager:    manager,
		metricsHandler: manager.MetricsHandler(),
	}

	collectRawMetricsFn = func(_ context.Context, _ *kubernetes.KubeClient) (*kubernetes.ClusterRawMetrics, error) {
		return nil, errors.New("collect raw metrics failed")
	}
	collectNodeConditionsFn = func(_ context.Context, _ *kubernetes.KubeClient) ([]kubernetes.NodeReadyStatus, error) {
		return nil, errors.New("collect node conditions failed")
	}
	collectEtcdHealthFn = func(_ context.Context, _ *kubernetes.KubeClient) (bool, error) {
		return false, errors.New("collect etcd health failed")
	}
	collectAPIServerCertFn = func(_ context.Context, _ *kubernetes.KubeClient) (*kubernetes.TLSCertInfo, error) {
		return nil, errors.New("collect tls cert failed")
	}
	collectAPIServerHealthFn = func(_ context.Context, _ *kubernetes.KubeClient) bool {
		return true
	}
	collectAPIServerLatencyFn = func(_ context.Context, _ *kubernetes.KubeClient) (kubernetes.APIServerLatencyHistogram, error) {
		return kubernetes.APIServerLatencyHistogram{
			Buckets: map[float64]float64{0.1: 40, math.Inf(1): 100},
			Count:   100,
		}, nil
	}

	service.pushPerformanceMetrics(context.Background())

	body := serveMetrics(t, service.metricsHandler)
	require.Contains(t, body, pkgmetrics.ClusterAPIServerRequestLatencySecondsBucketMetric+"{le=\"0.1\"} 40")
	require.Contains(t, body, pkgmetrics.ClusterAPIServerRequestLatencySecondsCountMetric+" 100")
}

func TestPushPerformanceMetricsRetainsAPIServerLatencyOnTransientFailure(t *testing.T) {
	oldCollectRawMetricsFn := collectRawMetricsFn
	oldCollectNodeConditionsFn := collectNodeConditionsFn
	oldCollectEtcdHealthFn := collectEtcdHealthFn
	oldCollectAPIServerCertFn := collectAPIServerCertFn
	oldCollectAPIServerHealthFn := collectAPIServerHealthFn
	oldCollectAPIServerLatencyFn := collectAPIServerLatencyFn
	t.Cleanup(func() {
		collectRawMetricsFn = oldCollectRawMetricsFn
		collectNodeConditionsFn = oldCollectNodeConditionsFn
		collectEtcdHealthFn = oldCollectEtcdHealthFn
		collectAPIServerCertFn = oldCollectAPIServerCertFn
		collectAPIServerHealthFn = oldCollectAPIServerHealthFn
		collectAPIServerLatencyFn = oldCollectAPIServerLatencyFn
	})

	manager := NewManager(&ManagerParameters{
		Options:           &agent.Options{DataPath: t.TempDir()},
		ContainerPlatform: agent.PlatformKubernetes,
	})
	service := &PollService{
		edgeManager:    manager,
		metricsHandler: manager.MetricsHandler(),
	}

	collectRawMetricsFn = func(_ context.Context, _ *kubernetes.KubeClient) (*kubernetes.ClusterRawMetrics, error) {
		return nil, errors.New("collect raw metrics failed")
	}
	collectNodeConditionsFn = func(_ context.Context, _ *kubernetes.KubeClient) ([]kubernetes.NodeReadyStatus, error) {
		return nil, errors.New("collect node conditions failed")
	}
	collectEtcdHealthFn = func(_ context.Context, _ *kubernetes.KubeClient) (bool, error) {
		return false, errors.New("collect etcd health failed")
	}
	collectAPIServerCertFn = func(_ context.Context, _ *kubernetes.KubeClient) (*kubernetes.TLSCertInfo, error) {
		return nil, errors.New("collect tls cert failed")
	}
	collectAPIServerHealthFn = func(_ context.Context, _ *kubernetes.KubeClient) bool {
		return true
	}

	collectAPIServerLatencyFn = func(_ context.Context, _ *kubernetes.KubeClient) (kubernetes.APIServerLatencyHistogram, error) {
		return kubernetes.APIServerLatencyHistogram{
			Buckets: map[float64]float64{0.1: 40, math.Inf(1): 100},
			Count:   100,
		}, nil
	}
	service.pushPerformanceMetrics(context.Background())

	collectAPIServerLatencyFn = func(_ context.Context, _ *kubernetes.KubeClient) (kubernetes.APIServerLatencyHistogram, error) {
		return kubernetes.APIServerLatencyHistogram{}, errors.New("latency scrape timeout")
	}
	service.pushPerformanceMetrics(context.Background())

	// A transient scrape error must retain the last published buckets so the
	// evaluator's rate() window tolerates the gap rather than seeing a reset.
	body := serveMetrics(t, service.metricsHandler)
	require.Contains(t, body, pkgmetrics.ClusterAPIServerRequestLatencySecondsBucketMetric+"{le=\"0.1\"} 40")
	require.Contains(t, body, pkgmetrics.ClusterAPIServerRequestLatencySecondsCountMetric+" 100")
}

func TestPushPerformanceMetricsClearsAPIServerLatencyWhenUnsupported(t *testing.T) {
	oldCollectRawMetricsFn := collectRawMetricsFn
	oldCollectNodeConditionsFn := collectNodeConditionsFn
	oldCollectEtcdHealthFn := collectEtcdHealthFn
	oldCollectAPIServerCertFn := collectAPIServerCertFn
	oldCollectAPIServerHealthFn := collectAPIServerHealthFn
	oldCollectAPIServerLatencyFn := collectAPIServerLatencyFn
	t.Cleanup(func() {
		collectRawMetricsFn = oldCollectRawMetricsFn
		collectNodeConditionsFn = oldCollectNodeConditionsFn
		collectEtcdHealthFn = oldCollectEtcdHealthFn
		collectAPIServerCertFn = oldCollectAPIServerCertFn
		collectAPIServerHealthFn = oldCollectAPIServerHealthFn
		collectAPIServerLatencyFn = oldCollectAPIServerLatencyFn
	})

	manager := NewManager(&ManagerParameters{
		Options:           &agent.Options{DataPath: t.TempDir()},
		ContainerPlatform: agent.PlatformKubernetes,
	})
	service := &PollService{
		edgeManager:    manager,
		metricsHandler: manager.MetricsHandler(),
	}

	collectRawMetricsFn = func(_ context.Context, _ *kubernetes.KubeClient) (*kubernetes.ClusterRawMetrics, error) {
		return nil, errors.New("collect raw metrics failed")
	}
	collectNodeConditionsFn = func(_ context.Context, _ *kubernetes.KubeClient) ([]kubernetes.NodeReadyStatus, error) {
		return nil, errors.New("collect node conditions failed")
	}
	collectEtcdHealthFn = func(_ context.Context, _ *kubernetes.KubeClient) (bool, error) {
		return false, errors.New("collect etcd health failed")
	}
	collectAPIServerCertFn = func(_ context.Context, _ *kubernetes.KubeClient) (*kubernetes.TLSCertInfo, error) {
		return nil, errors.New("collect tls cert failed")
	}
	collectAPIServerHealthFn = func(_ context.Context, _ *kubernetes.KubeClient) bool {
		return true
	}

	collectAPIServerLatencyFn = func(_ context.Context, _ *kubernetes.KubeClient) (kubernetes.APIServerLatencyHistogram, error) {
		return kubernetes.APIServerLatencyHistogram{
			Buckets: map[float64]float64{0.1: 40, math.Inf(1): 100},
			Count:   100,
		}, nil
	}
	service.pushPerformanceMetrics(context.Background())
	require.Contains(t, serveMetrics(t, service.metricsHandler), pkgmetrics.ClusterAPIServerRequestLatencySecondsBucketMetric+"{")

	collectAPIServerLatencyFn = func(_ context.Context, _ *kubernetes.KubeClient) (kubernetes.APIServerLatencyHistogram, error) {
		return kubernetes.APIServerLatencyHistogram{}, kubernetes.ErrAPIServerRequestLatencyUnsupported
	}
	service.pushPerformanceMetrics(context.Background())

	body := serveMetrics(t, service.metricsHandler)
	require.NotContains(t, body, pkgmetrics.ClusterAPIServerRequestLatencySecondsBucketMetric+"{")
	require.Contains(t, body, pkgmetrics.ClusterAPIServerRequestLatencySecondsCountMetric+" 0")
}

func TestMaybeReloadRulesRetriesAfterFilesystemFailure(t *testing.T) {
	invalidDataPath := filepath.Join(t.TempDir(), "data-path-file")
	require.NoError(t, os.WriteFile(invalidDataPath, []byte("not a directory"), 0o600))

	manager := NewManager(&ManagerParameters{
		Options: &agent.Options{DataPath: invalidDataPath},
	})
	eval, err := evaluator.New(evaluator.Config{
		DataDir:    filepath.Join(t.TempDir(), "evaluator-data"),
		EndpointID: 1,
	})
	require.NoError(t, err)
	eval.Start()
	t.Cleanup(eval.Stop)

	const validRulesYAML = `groups:
  - name: portainer-edge-cluster-alerts
    rules:
      - alert: HighCPU
        expr: vector(1)
`

	service := &PollService{
		edgeManager:    manager,
		evaluator:      eval,
		alertRulesYAML: validRulesYAML,
		metricsHandler: manager.MetricsHandler(),
	}

	service.maybeReloadRules()
	require.Contains(t, service.configReloadError, "create alerting dir")
	require.Zero(t, service.alertRulesHash)

	retryDataPath := t.TempDir()
	manager.agentOptions.DataPath = retryDataPath

	service.maybeReloadRules()

	require.Empty(t, service.configReloadError)
	require.Equal(t, computeRulesHash(validRulesYAML), service.alertRulesHash)
	_, err = os.Stat(filepath.Join(retryDataPath, "alerting", "alerts.yaml"))
	require.NoError(t, err)
}

func serveMetrics(t *testing.T, h *agentmetrics.Handler) string {
	t.Helper()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/metrics", nil)
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	return rec.Body.String()
}
