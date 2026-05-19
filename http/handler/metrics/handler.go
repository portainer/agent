package metrics

import (
	"net/http"
	"sync"

	"github.com/portainer/agent/kubernetes"
	pkgmetrics "github.com/portainer/portainer/pkg/metrics"
	prometheusreg "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var metricNames = []string{
	pkgmetrics.ClusterCPUUsageCoresMetric,
	pkgmetrics.ClusterCPUCapacityCoresMetric,
	pkgmetrics.ClusterMemoryWorkingSetBytesMetric,
	pkgmetrics.ClusterMemoryCapacityBytesMetric,
	pkgmetrics.ClusterFilesystemUsedBytesMetric,
	pkgmetrics.ClusterFilesystemCapacityBytesMetric,
	pkgmetrics.ClusterNetworkReceiveBytesMetric,
	pkgmetrics.ClusterNetworkTransmitBytesMetric,
}

// Handler serves Prometheus exposition-format metrics at /api/metrics.
// It maintains the latest successful cluster metrics snapshot for exposition.
type Handler struct {
	handler http.Handler

	mu                     sync.RWMutex
	descriptors            map[string]*prometheusreg.Desc
	values                 map[string]float64
	nodeReady              *prometheusreg.GaugeVec
	nodeUnschedulable      *prometheusreg.GaugeVec
	etcdHealthy            prometheusreg.Gauge
	etcdHealthValid        prometheusreg.Gauge
	apiServerTLSCertExpiry *prometheusreg.GaugeVec
	apiServerHealthy       prometheusreg.Gauge
}

// NewHandler creates a new metrics handler with a dedicated Prometheus registry.
func NewHandler() *Handler {
	reg := prometheusreg.NewRegistry()
	nodeReady := prometheusreg.NewGaugeVec(prometheusreg.GaugeOpts{
		Name: pkgmetrics.ClusterNodeReadyMetric,
		Help: "1 if the node is Ready, 0 if NotReady",
	}, []string{"node"})
	reg.MustRegister(nodeReady)

	nodeUnschedulable := prometheusreg.NewGaugeVec(prometheusreg.GaugeOpts{
		Name: pkgmetrics.ClusterNodeUnschedulableMetric,
		Help: "1 if the node is cordoned (unschedulable), 0 otherwise",
	}, []string{"node"})
	reg.MustRegister(nodeUnschedulable)

	etcdHealthy := prometheusreg.NewGauge(prometheusreg.GaugeOpts{
		Name: pkgmetrics.ClusterEtcdHealthyMetric,
		Help: "1 if the API server can reach etcd, 0 otherwise",
	})
	reg.MustRegister(etcdHealthy)

	etcdHealthValid := prometheusreg.NewGauge(prometheusreg.GaugeOpts{
		Name: pkgmetrics.ClusterEtcdHealthValidMetric,
		Help: "1 if etcd health is based on a definitive API server check, 0 otherwise",
	})
	reg.MustRegister(etcdHealthValid)

	tlsCertExpiry := prometheusreg.NewGaugeVec(prometheusreg.GaugeOpts{
		Name: pkgmetrics.ClusterAPIServerTLSCertExpirySecondsMetric,
		Help: "Unix timestamp of TLS certificate NotAfter. Labels: source, cn.",
	}, []string{"source", "cn"})
	reg.MustRegister(tlsCertExpiry)

	apiServerHealthy := prometheusreg.NewGauge(prometheusreg.GaugeOpts{
		Name: pkgmetrics.ClusterAPIServerHealthyMetric,
		Help: "1 if the API server reports healthy on /livez, 0 otherwise",
	})
	reg.MustRegister(apiServerHealthy)

	descriptors := make(map[string]*prometheusreg.Desc, len(metricNames))
	for _, name := range metricNames {
		descriptors[name] = prometheusreg.NewDesc(name, "Edge agent cluster metric: "+name, nil, nil)
	}

	h := &Handler{
		descriptors:            descriptors,
		values:                 make(map[string]float64),
		nodeReady:              nodeReady,
		nodeUnschedulable:      nodeUnschedulable,
		etcdHealthy:            etcdHealthy,
		etcdHealthValid:        etcdHealthValid,
		apiServerTLSCertExpiry: tlsCertExpiry,
		apiServerHealthy:       apiServerHealthy,
	}
	reg.MustRegister(h)

	h.handler = promhttp.HandlerFor(reg, promhttp.HandlerOpts{})

	return h
}

// UpdateMetrics sets gauge values from raw Kubernetes cluster metrics.
func (h *Handler) UpdateMetrics(raw *kubernetes.ClusterRawMetrics) {
	snapshot := make(map[string]float64, len(metricNames))
	if raw != nil {
		if raw.HasCPU {
			snapshot[pkgmetrics.ClusterCPUUsageCoresMetric] = float64(raw.CPUUsageNanoCores) / 1e9
			snapshot[pkgmetrics.ClusterCPUCapacityCoresMetric] = float64(raw.CPUCapacityNanoCores) / 1e9
		}

		if raw.HasMemory {
			snapshot[pkgmetrics.ClusterMemoryWorkingSetBytesMetric] = float64(raw.MemoryWorkingSetBytes)
			snapshot[pkgmetrics.ClusterMemoryCapacityBytesMetric] = float64(raw.MemoryCapacityBytes)
		}

		if raw.HasDisk {
			snapshot[pkgmetrics.ClusterFilesystemUsedBytesMetric] = float64(raw.DiskUsedBytes)
			snapshot[pkgmetrics.ClusterFilesystemCapacityBytesMetric] = float64(raw.DiskCapacityBytes)
		}

		if raw.HasNetwork {
			snapshot[pkgmetrics.ClusterNetworkReceiveBytesMetric] = float64(raw.NetworkRxBytes)
			snapshot[pkgmetrics.ClusterNetworkTransmitBytesMetric] = float64(raw.NetworkTxBytes)
		}
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	h.values = snapshot
}

func boolToFloat64(b bool) float64 {
	if b {
		return 1.0
	}

	return 0.0
}

// UpdateNodeMetrics sets per-node readiness and unschedulable gauges.
func (h *Handler) UpdateNodeMetrics(statuses []kubernetes.NodeReadyStatus) {
	h.nodeReady.Reset()
	h.nodeUnschedulable.Reset()

	for _, status := range statuses {
		h.nodeReady.WithLabelValues(status.Name).Set(boolToFloat64(status.Ready))
		h.nodeUnschedulable.WithLabelValues(status.Name).Set(boolToFloat64(status.Unschedulable))
	}
}

// ClearNodeMetrics removes all published per-node gauges.
func (h *Handler) ClearNodeMetrics() {
	h.nodeReady.Reset()
	h.nodeUnschedulable.Reset()
}

// UpdateEtcdMetrics sets the etcd health gauge.
func (h *Handler) UpdateEtcdMetrics(healthy bool) {
	h.etcdHealthy.Set(boolToFloat64(healthy))
	h.etcdHealthValid.Set(1)
}

// ClearEtcdMetrics marks etcd health as indeterminate until a definitive
// etcd check result is observed again.
func (h *Handler) ClearEtcdMetrics() {
	h.etcdHealthValid.Set(0)
}

// UpdateAPIServerTLSCertMetrics sets per-certificate expiry gauges for the Kubernetes API server.
func (h *Handler) UpdateAPIServerTLSCertMetrics(certs []kubernetes.TLSCertInfo) {
	h.apiServerTLSCertExpiry.Reset()

	for _, cert := range certs {
		h.apiServerTLSCertExpiry.WithLabelValues(cert.Source, cert.CN).Set(float64(cert.NotAfter.Unix()))
	}
}

// ClearAPIServerTLSCertMetrics removes all published API server TLS cert gauges.
func (h *Handler) ClearAPIServerTLSCertMetrics() {
	h.apiServerTLSCertExpiry.Reset()
}

// UpdateAPIServerHealthMetrics sets the API server health gauge.
func (h *Handler) UpdateAPIServerHealthMetrics(healthy bool) {
	h.apiServerHealthy.Set(boolToFloat64(healthy))
}

// ClearMetrics removes the currently published metric snapshot.
func (h *Handler) ClearMetrics() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.values = make(map[string]float64)
}

func (h *Handler) Describe(ch chan<- *prometheusreg.Desc) {
	for _, name := range metricNames {
		ch <- h.descriptors[name]
	}
}

func (h *Handler) Collect(ch chan<- prometheusreg.Metric) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, name := range metricNames {
		value, ok := h.values[name]
		if !ok {
			continue
		}

		vt := prometheusreg.GaugeValue
		if name == pkgmetrics.ClusterNetworkReceiveBytesMetric || name == pkgmetrics.ClusterNetworkTransmitBytesMetric {
			// Network bytes are cumulative counters from the kubelet stats/summary
			// endpoint; they must be exported with CounterValue to match the _total
			// suffix in their metric names.
			vt = prometheusreg.CounterValue
		}

		ch <- prometheusreg.MustNewConstMetric(h.descriptors[name], vt, value)
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.handler.ServeHTTP(w, r)
}
