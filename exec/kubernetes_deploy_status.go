package exec

import (
	"context"
	"errors"
	"io"
	"os"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes/scheme"

	"github.com/portainer/agent"
	"github.com/portainer/agent/kubernetes"
	libstack "github.com/portainer/portainer/pkg/libstack"
	"github.com/rs/zerolog/log"
)

func (service *KubernetesDeployer) WaitForStatus(ctx context.Context, name string, requiredStatus libstack.Status, options agent.CheckStatusOptions) <-chan libstack.WaitResult {
	waitResultCh := make(chan libstack.WaitResult)

	go func() {
		for {
			select {
			case <-ctx.Done():
				waitResultCh <- libstack.WaitResult{Status: requiredStatus, ErrorMsg: "failed to wait for status: " + ctx.Err().Error()}
			default:
			}

			time.Sleep(1 * time.Second)

			status, workloadError, err := service.getStatusForYAML(requiredStatus, options)
			if err != nil {
				log.Warn().
					Str("project_name", name).
					Err(err).
					Msg("failed to get workload status")
				waitResultCh <- libstack.WaitResult{Status: requiredStatus, ErrorMsg: "failed to wait for status: " + err.Error()}
				return
			}

			if status == requiredStatus {
				waitResultCh <- libstack.WaitResult{Status: requiredStatus}
				return
			}

			if status == libstack.StatusCompleted && requiredStatus == libstack.StatusRunning {
				waitResultCh <- libstack.WaitResult{Status: status}
				return
			}

			if workloadError != "" {
				waitResultCh <- libstack.WaitResult{Status: requiredStatus, ErrorMsg: workloadError}
				return
			}

			log.Debug().
				Str("project_name", name).
				Str("required_status", string(requiredStatus)).
				Str("status", string(status)).
				Msg("waiting for status")
		}
	}()

	return waitResultCh
}

func (service *KubernetesDeployer) getStatusForYAML(requiredStatus libstack.Status, options agent.CheckStatusOptions) (libstack.Status, string, error) {
	// open the YAML file
	file, err := os.Open(options.StackFileLocation)
	if err != nil {
		return libstack.StatusError, "", err
	}
	defer file.Close()

	defaultNamespace := options.Namespace

	// create YAML to JSON decoder
	yamlDecoder := yaml.NewYAMLOrJSONDecoder(file, 4096)
	// create kubernetes object parser
	decoder := scheme.Codecs.UniversalDeserializer()

	statuses := make([]kubernetes.ResourceStatus, 0)
	// loop to handle multiple objects in the YAML file
	for {
		var raw runtime.RawExtension
		// decode individual YAML object as raw
		err := yamlDecoder.Decode(&raw)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			log.Warn().Err(err).Msg("error parsing YAML object. Skip evaluating its status")
			continue
		}

		// from the raw, get a real runtime object that can be cast
		obj, gvk, err := decoder.Decode(raw.Raw, nil, nil)
		if err != nil {
			log.Warn().Err(err).Any("obj", obj).Msg("error decoding kubernetes object. Skip evaluating its status")
			continue
		}

		// verify that the decoded object is known by kube
		if gvk.Empty() {
			log.Warn().Any("obj", obj).Msg("found empty GroupVersionKind for object. Skip evaluating its status")
			continue
		}

		var status kubernetes.ResourceStatus

		switch o := obj.(type) {
		case *appsv1.Deployment:
			ns := defaultNamespace
			if o.Namespace != "" {
				ns = o.Namespace
			}
			status = service.kubeClient.GetDeploymentStatus(ns, o.Name)
		case *appsv1.DaemonSet:
			ns := defaultNamespace
			if o.Namespace != "" {
				ns = o.Namespace
			}
			status = service.kubeClient.GetDaemonSetStatus(ns, o.Name)
		case *appsv1.StatefulSet:
			ns := defaultNamespace
			if o.Namespace != "" {
				ns = o.Namespace
			}
			status = service.kubeClient.GetStatefulSetStatus(ns, o.Name)
		case *corev1.Pod:
			ns := defaultNamespace
			if o.Namespace != "" {
				ns = o.Namespace
			}
			status = service.kubeClient.GetPodStatus(ns, o.Name)
		default:
			continue
		}

		// - removed workload will error on Kubernetes API query (empty workload item or empty associated pods list)
		//   these errors are raised as { Status: StatusRemoved, Err: != nil }
		//   we only consider them to be real API errors when requiredStatus != StatusRemoved
		// - other runtime errors are raised as { Status: StatusError, Err: != nil }
		// - workloads in an errored state are raised as { Status: StatusError, Message: != "", Err: nil }
		if status.Err != nil && (status.Status != libstack.StatusRemoved || requiredStatus != libstack.StatusRemoved) {
			log.Warn().Err(status.Err).Str("message", status.Message).Msg("error while retrieving workload status")
			return libstack.StatusError, status.Message, status.Err
		}
		statuses = append(statuses, status)
	}

	aggregatedStatus := kubernetes.AggregateStatuses(statuses)
	return aggregatedStatus.Status, aggregatedStatus.Message, nil
}
