package metrics

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/portainer/agent/kubernetes"
	pkgmetrics "github.com/portainer/portainer/pkg/metrics"
	"github.com/stretchr/testify/require"
)

func TestUpdateMetricsReplacesPublishedSnapshot(t *testing.T) {
	h := NewHandler()

	h.UpdateMetrics(&kubernetes.ClusterRawMetrics{
		HasCPU:               true,
		CPUUsageNanoCores:    2_000_000_000,
		CPUCapacityNanoCores: 4_000_000_000,
	})

	body := serveMetrics(t, h)
	require.Contains(t, body, pkgmetrics.ClusterCPUUsageCoresMetric)
	require.NotContains(t, body, pkgmetrics.ClusterMemoryWorkingSetBytesMetric)

	h.UpdateMetrics(&kubernetes.ClusterRawMetrics{
		HasMemory:             true,
		MemoryWorkingSetBytes: 1024,
		MemoryCapacityBytes:   2048,
	})

	body = serveMetrics(t, h)
	require.NotContains(t, body, pkgmetrics.ClusterCPUUsageCoresMetric)
	require.Contains(t, body, pkgmetrics.ClusterMemoryWorkingSetBytesMetric)
}

func TestClearMetricsRemovesPublishedSnapshot(t *testing.T) {
	h := NewHandler()
	h.UpdateMetrics(&kubernetes.ClusterRawMetrics{
		HasCPU:               true,
		CPUUsageNanoCores:    1_000_000_000,
		CPUCapacityNanoCores: 2_000_000_000,
	})

	h.ClearMetrics()

	body := serveMetrics(t, h)
	require.NotContains(t, body, pkgmetrics.ClusterCPUUsageCoresMetric)
	require.Contains(t, body, pkgmetrics.ClusterEtcdHealthyMetric+" 0")
	require.Contains(t, body, pkgmetrics.ClusterEtcdHealthValidMetric+" 0")
}

func TestUpdateEtcdMetricsSetsGauge(t *testing.T) {
	h := NewHandler()

	h.UpdateEtcdMetrics(true)
	body := serveMetrics(t, h)
	require.Contains(t, body, pkgmetrics.ClusterEtcdHealthyMetric+" 1")
	require.Contains(t, body, pkgmetrics.ClusterEtcdHealthValidMetric+" 1")

	h.UpdateEtcdMetrics(false)
	body = serveMetrics(t, h)
	require.Contains(t, body, pkgmetrics.ClusterEtcdHealthyMetric+" 0")
	require.Contains(t, body, pkgmetrics.ClusterEtcdHealthValidMetric+" 1")
}

func TestClearEtcdMetricsMarksGaugeAsIndeterminate(t *testing.T) {
	h := NewHandler()

	h.UpdateEtcdMetrics(true)
	require.Contains(t, serveMetrics(t, h), pkgmetrics.ClusterEtcdHealthValidMetric+" 1")

	h.ClearEtcdMetrics()
	body := serveMetrics(t, h)
	require.Contains(t, body, pkgmetrics.ClusterEtcdHealthValidMetric+" 0")
}

func TestUpdateNodeMetricsReplacesPublishedSeries(t *testing.T) {
	h := NewHandler()

	h.UpdateNodeMetrics([]kubernetes.NodeReadyStatus{
		{Name: "node-a", Ready: true, Unschedulable: false},
		{Name: "node-b", Ready: false, Unschedulable: true},
	})

	body := serveMetrics(t, h)
	require.Contains(t, body, pkgmetrics.ClusterNodeReadyMetric+"{node=\"node-a\"} 1")
	require.Contains(t, body, pkgmetrics.ClusterNodeReadyMetric+"{node=\"node-b\"} 0")
	require.Contains(t, body, pkgmetrics.ClusterNodeUnschedulableMetric+"{node=\"node-a\"} 0")
	require.Contains(t, body, pkgmetrics.ClusterNodeUnschedulableMetric+"{node=\"node-b\"} 1")

	h.UpdateNodeMetrics([]kubernetes.NodeReadyStatus{{Name: "node-b", Ready: true, Unschedulable: false}})

	body = serveMetrics(t, h)
	require.NotContains(t, body, pkgmetrics.ClusterNodeReadyMetric+"{node=\"node-a\"}")
	require.Contains(t, body, pkgmetrics.ClusterNodeReadyMetric+"{node=\"node-b\"} 1")
	require.NotContains(t, body, pkgmetrics.ClusterNodeUnschedulableMetric+"{node=\"node-a\"}")
	require.Contains(t, body, pkgmetrics.ClusterNodeUnschedulableMetric+"{node=\"node-b\"} 0")
}

func TestClearNodeMetricsKeepsRawSnapshot(t *testing.T) {
	h := NewHandler()

	h.UpdateMetrics(&kubernetes.ClusterRawMetrics{
		HasCPU:               true,
		CPUUsageNanoCores:    2_000_000_000,
		CPUCapacityNanoCores: 4_000_000_000,
	})
	h.UpdateNodeMetrics([]kubernetes.NodeReadyStatus{{Name: "node-a", Ready: false, Unschedulable: true}})

	h.ClearNodeMetrics()

	body := serveMetrics(t, h)
	require.Contains(t, body, pkgmetrics.ClusterCPUUsageCoresMetric)
	require.NotContains(t, body, pkgmetrics.ClusterNodeReadyMetric)
	require.NotContains(t, body, pkgmetrics.ClusterNodeUnschedulableMetric)
}

func TestUpdateTLSCertMetricsPublishesSeries(t *testing.T) {
	h := NewHandler()
	expiry := time.Unix(1_800_000_000, 0)

	h.UpdateAPIServerTLSCertMetrics([]kubernetes.TLSCertInfo{{
		Source:   "apiserver",
		CN:       "kube-apiserver",
		NotAfter: expiry,
	}})

	body := serveMetrics(t, h)
	require.Contains(t, body, pkgmetrics.ClusterAPIServerTLSCertExpirySecondsMetric)
	require.Contains(t, body, "source=\"apiserver\"")
	require.Contains(t, body, "cn=\"kube-apiserver\"")
}

func TestUpdateTLSCertMetricsReplacesPublishedSeries(t *testing.T) {
	h := NewHandler()

	h.UpdateAPIServerTLSCertMetrics([]kubernetes.TLSCertInfo{{
		Source:   "apiserver",
		CN:       "old-cn",
		NotAfter: time.Unix(1_800_000_000, 0),
	}})

	h.UpdateAPIServerTLSCertMetrics([]kubernetes.TLSCertInfo{{
		Source:   "apiserver",
		CN:       "new-cn",
		NotAfter: time.Unix(1_900_000_000, 0),
	}})

	body := serveMetrics(t, h)
	require.NotContains(t, body, "cn=\"old-cn\"")
	require.Contains(t, body, "cn=\"new-cn\"")
}

func TestClearTLSCertMetricsRemovesPublishedSeries(t *testing.T) {
	h := NewHandler()

	h.UpdateMetrics(&kubernetes.ClusterRawMetrics{
		HasCPU:               true,
		CPUUsageNanoCores:    1_000_000_000,
		CPUCapacityNanoCores: 2_000_000_000,
	})
	h.UpdateAPIServerTLSCertMetrics([]kubernetes.TLSCertInfo{{
		Source:   "apiserver",
		CN:       "kube-apiserver",
		NotAfter: time.Unix(1_800_000_000, 0),
	}})

	h.ClearAPIServerTLSCertMetrics()

	body := serveMetrics(t, h)
	require.Contains(t, body, pkgmetrics.ClusterCPUUsageCoresMetric)
	require.NotContains(t, body, pkgmetrics.ClusterAPIServerTLSCertExpirySecondsMetric)
}

func serveMetrics(t *testing.T, h *Handler) string {
	t.Helper()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/metrics", nil)
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	return rec.Body.String()
}
