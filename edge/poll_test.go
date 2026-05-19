package edge

import (
	"context"
	"errors"
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
	pkgmetrics "github.com/portainer/portainer/pkg/metrics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestBuildMetricsScrapeTargetUsesConfiguredHostPort(t *testing.T) {
	assert.Equal(t, "http://127.0.0.1:9001/api/metrics", buildMetricsScrapeTarget("127.0.0.1:9001"))
}

func TestBuildMetricsScrapeTargetNormalizesUnspecifiedHost(t *testing.T) {
	tests := map[string]string{
		":9001":        "http://localhost:9001/api/metrics",
		"0.0.0.0:9001": "http://localhost:9001/api/metrics",
		"[::]:9001":    "http://localhost:9001/api/metrics",
	}

	for input, expected := range tests {
		t.Run(input, func(t *testing.T) {
			assert.Equal(t, expected, buildMetricsScrapeTarget(input))
		})
	}
}

func TestComputeRulesHashSameContentSameHash(t *testing.T) {
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
	hashA := computeRulesHash("")
	hashB := computeRulesHash("")
	assert.Equal(t, hashA, hashB)
}

func TestBuildEdgeAlertStateSortsRules(t *testing.T) {
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
		edgeStackManager: stack.NewStackManager(mockClient, "", nil, "edge-id", nil),
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
		return true
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
