package kubernetes

import (
	"errors"
	"sync/atomic"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/stretchr/testify/require"
)

func makeNode(name string, cpuMillis int64, memBytes int64) corev1.Node {
	return corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: corev1.NodeStatus{
			Capacity: corev1.ResourceList{
				corev1.ResourceCPU:    *resource.NewMilliQuantity(cpuMillis, resource.DecimalSI),
				corev1.ResourceMemory: *resource.NewQuantity(memBytes, resource.BinarySI),
			},
		},
	}
}

func TestAggregateClusterPerformanceMetrics_DiskPopulatedWhenFsDataPresent(t *testing.T) {
	t.Parallel()
	nodes := []corev1.Node{makeNode("node1", 4000, 8*1024*1024*1024)}

	collectFn := func(_ corev1.Node) (*nodePerformanceSample, error) {
		return &nodePerformanceSample{
			DiskUsedBytes:     50 * 1024 * 1024 * 1024,
			DiskCapacityBytes: 100 * 1024 * 1024 * 1024,
			HasDisk:           true,
		}, nil
	}

	metrics, err := aggregateClusterPerformanceMetrics(nodes, collectFn)
	require.NoError(t, err)
	require.NotNil(t, metrics)
	require.Equal(t, 50, int(metrics.DiskUsage))
}

func TestAggregateClusterPerformanceMetrics_DiskZeroWhenFsDataAbsent(t *testing.T) {
	t.Parallel()
	nodes := []corev1.Node{makeNode("node1", 4000, 8*1024*1024*1024)}

	collectFn := func(_ corev1.Node) (*nodePerformanceSample, error) {
		return &nodePerformanceSample{
			CPUUsageNanoCores:    1_000_000_000,
			CPUCapacityNanoCores: 4_000_000_000,
			HasCPU:               true,
		}, nil
	}

	metrics, err := aggregateClusterPerformanceMetrics(nodes, collectFn)
	require.NoError(t, err)
	require.NotNil(t, metrics)
	require.Equal(t, 0, int(metrics.DiskUsage))
}

func TestAggregateClusterPerformanceMetrics_DiskAggregatedAcrossMultipleNodes(t *testing.T) {
	t.Parallel()
	twoNodes := []corev1.Node{
		makeNode("node1", 4000, 8*1024*1024*1024),
		makeNode("node2", 4000, 8*1024*1024*1024),
	}

	var counter atomic.Int64
	collectFn := func(_ corev1.Node) (*nodePerformanceSample, error) {
		i := counter.Add(1)
		used := uint64(i) * 25 * 1024 * 1024 * 1024
		return &nodePerformanceSample{
			DiskUsedBytes:     used,
			DiskCapacityBytes: 100 * 1024 * 1024 * 1024,
			HasDisk:           true,
		}, nil
	}

	// node1: 25/100 GB, node2: 50/100 GB -> total 75/200 = 37.5 -> rounds to 38
	metrics, err := aggregateClusterPerformanceMetrics(twoNodes, collectFn)
	require.NoError(t, err)
	require.Equal(t, 38, int(metrics.DiskUsage))
}

func TestAggregateClusterPerformanceMetrics_AllNodesFail(t *testing.T) {
	t.Parallel()
	nodes := []corev1.Node{makeNode("node1", 4000, 8*1024*1024*1024)}

	collectFn := func(_ corev1.Node) (*nodePerformanceSample, error) {
		return nil, errors.New("connection refused")
	}

	_, err := aggregateClusterPerformanceMetrics(nodes, collectFn)
	require.Error(t, err)
}
