package kubernetes

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/portainer/portainer/pkg/libstack"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
	utilexec "k8s.io/client-go/util/exec"
)

// KubeClient can be used to query the Kubernetes API
type KubeClient struct {
	cli *kubernetes.Clientset
	// dynamicCli enables access to arbitrary resources (including CRDs)
	dynamicCli dynamic.Interface
	config     *rest.Config
}

// NewKubeClient returns a pointer to a new KubeClient instance
func NewKubeClient() (*KubeClient, error) {
	kubeCli := &KubeClient{}

	cli, err := BuildLocalClient()
	if err != nil {
		return nil, err
	}

	config, err := buildKubeConfig()
	if err != nil {
		return nil, err
	}

	// dynamic client needed for GetResource/PatchResource to work with any K8s resource type
	dynamicCli, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic client: %w", err)
	}

	kubeCli.cli = cli
	kubeCli.dynamicCli = dynamicCli
	kubeCli.config = config
	return kubeCli, nil
}

// buildKubeConfig builds the Kubernetes configuration for the client
func buildKubeConfig() (*rest.Config, error) {
	// Developers can set the DEV_KUBECONFIG_PATH to run agent locally
	devKubeConfigPath := os.Getenv("DEV_KUBECONFIG_PATH")
	if devKubeConfigPath != "" {
		return clientcmd.BuildConfigFromFlags("", devKubeConfigPath)
	}
	return rest.InClusterConfig()
}

func BuildLocalClient() (*kubernetes.Clientset, error) {
	var config *rest.Config
	var err error

	// Developers can set the DEV_KUBECONFIG_PATH to run agent locally
	devKubeConfigPath := os.Getenv("DEV_KUBECONFIG_PATH")
	if devKubeConfigPath != "" {
		config, err = clientcmd.BuildConfigFromFlags("", devKubeConfigPath)
		if err != nil {
			return nil, fmt.Errorf("failed to build config from kubeconfig file %s: %w", devKubeConfigPath, err)
		}
	} else {
		config, err = rest.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to build in-cluster config: %w", err)
		}
	}

	return kubernetes.NewForConfig(config)
}

// ExecProcessParams holds parameters for StartExecProcess.
type ExecProcessParams struct {
	Token         string
	Namespace     string
	PodName       string
	ContainerName string
	Command       []string
	Stdin         io.Reader                       // bound to the exec process stdin
	Stdout        io.Writer                       // receives exec process output
	ResizeQueue   remotecommand.TerminalSizeQueue // nil if resize not needed
}

// StartExecProcess starts an exec process inside a container.
func (kcl *KubeClient) StartExecProcess(params ExecProcessParams) error {
	config := rest.CopyConfig(kcl.config)

	if params.Token != "" {
		config.BearerToken = params.Token
		config.BearerTokenFile = ""
	}

	req := kcl.cli.CoreV1().RESTClient().
		Post().
		Resource("pods").
		Name(params.PodName).
		Namespace(params.Namespace).
		SubResource("exec")

	req.VersionedParams(&corev1.PodExecOptions{
		Container: params.ContainerName,
		Command:   params.Command,
		Stdin:     true,
		Stdout:    true,
		Stderr:    true,
		TTY:       true,
	}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
	if err != nil {
		return err
	}

	err = exec.Stream(remotecommand.StreamOptions{
		Stdin:             params.Stdin,
		Stdout:            params.Stdout,
		Tty:               true,
		TerminalSizeQueue: params.ResizeQueue,
	})
	if err != nil {
		var exitError utilexec.ExitError
		if !errors.As(err, &exitError) {
			return errors.New("unable to start exec process")
		}
	}

	return nil
}

type ResourceStatus struct {
	Status  libstack.Status
	Message string
	Err     error
}

func (kcl *KubeClient) GetDeploymentStatus(namespace string, name string) ResourceStatus {
	item, err := kcl.cli.AppsV1().Deployments(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return ResourceStatus{
			Status:  libstack.StatusRemoved,
			Message: fmt.Sprintf("unable to retrieve deployment name=%s namespace=%s", name, namespace),
			Err:     err,
		}
	}

	if item.ObjectMeta.DeletionTimestamp != nil && item.Status.Replicas != 0 {
		return ResourceStatus{
			Status: libstack.StatusRemoving,
		}
	}

	if item.Spec.Replicas != nil && *item.Spec.Replicas == 0 && item.Status.Replicas == 0 {
		return ResourceStatus{
			Status: libstack.StatusStopped,
		}
	}

	// starting | running | completed | error
	return kcl.inferWorkloadStatusByPods(namespace, name, item.Spec.Selector)
}

func (kcl *KubeClient) GetDaemonSetStatus(namespace string, name string) ResourceStatus {
	item, err := kcl.cli.AppsV1().DaemonSets(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return ResourceStatus{
			Status:  libstack.StatusRemoved,
			Message: fmt.Sprintf("unable to retrieve daemonset name=%s namespace=%s", name, namespace),
			Err:     err,
		}
	}

	if item.ObjectMeta.DeletionTimestamp != nil && item.Status.CurrentNumberScheduled != 0 {
		return ResourceStatus{
			Status: libstack.StatusRemoving,
		}
	}

	// daemonset cannot be stopped

	// starting | running | completed | error
	return kcl.inferWorkloadStatusByPods(namespace, name, item.Spec.Selector)
}

func (kcl *KubeClient) GetStatefulSetStatus(namespace string, name string) ResourceStatus {
	item, err := kcl.cli.AppsV1().StatefulSets(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return ResourceStatus{
			Status:  libstack.StatusRemoved,
			Message: fmt.Sprintf("unable to retrieve statefulset name=%s namespace=%s", name, namespace),
			Err:     err,
		}
	}

	if item.ObjectMeta.DeletionTimestamp != nil && item.Status.Replicas != 0 {
		return ResourceStatus{
			Status: libstack.StatusRemoving,
		}
	}

	if item.Spec.Replicas != nil && *item.Spec.Replicas == 0 && item.Status.Replicas == 0 {
		return ResourceStatus{
			Status: libstack.StatusStopped,
		}
	}

	// starting | running | completed | error
	return kcl.inferWorkloadStatusByPods(namespace, name, item.Spec.Selector)
}

func (kcl *KubeClient) GetPodStatus(namespace string, name string) ResourceStatus {
	item, err := kcl.cli.CoreV1().Pods(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return ResourceStatus{
			Status:  libstack.StatusRemoved,
			Message: fmt.Sprintf("unable to retrieve pod name=%s namespace=%s", name, namespace),
			Err:     err,
		}
	}

	if item.ObjectMeta.DeletionTimestamp != nil {
		return ResourceStatus{
			Status: libstack.StatusRemoving,
		}
	}
	// pod cannot be stopped

	// starting | running | completed | error
	return kcl.inferWorkloadStatusByPods(namespace, name, metav1.SetAsLabelSelector(item.Labels))
}

// retrieve the starting | running | completed | error status of the workload based on pods states
func (kcl *KubeClient) inferWorkloadStatusByPods(namespace string, name string, selector *metav1.LabelSelector) ResourceStatus {
	labelSelector, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return ResourceStatus{
			Status:  libstack.StatusError,
			Message: fmt.Sprintf("unable to convert workload labelselector of name=%s namespace=%s to pods selector", name, namespace),
			Err:     err,
		}
	}

	pods, err := kcl.cli.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{LabelSelector: labelSelector.String()})
	if err != nil {
		return ResourceStatus{
			Status:  libstack.StatusRemoved,
			Message: fmt.Sprintf("unable to retrieve the pods list for name=%s namespace=%s", name, namespace),
			Err:     err,
		}
	}

	containerStatuses := make([]ResourceStatus, 0)

	for _, pod := range pods.Items {
		for _, status := range pod.Status.ContainerStatuses {
			if status.Ready || status.State.Running != nil {
				containerStatuses = append(containerStatuses, ResourceStatus{
					Status: libstack.StatusRunning,
				})
				continue
			}

			if status.RestartCount == 0 && status.State.Waiting != nil {
				containerStatuses = append(containerStatuses, ResourceStatus{
					Status:  libstack.StatusStarting,
					Message: status.State.Waiting.Message,
				})
				continue
			}

			state := status.LastTerminationState.Terminated
			if status.State.Terminated != nil {
				state = status.State.Terminated
			}

			if state != nil {
				reason := podLastTerminationReasonToLibstackStatus(state.Reason)
				message := podMessageFromContainerState(reason, status.State)
				containerStatuses = append(containerStatuses, ResourceStatus{
					Status:  reason,
					Message: message,
				})
				continue
			}

			containerStatuses = append(containerStatuses, ResourceStatus{
				Status: libstack.StatusUnknown,
			})
		}
	}

	return AggregateStatuses(containerStatuses)
}

func AggregateStatuses(statuses []ResourceStatus) ResourceStatus {
	statusCounts := make(map[libstack.Status]int)
	totalCount := len(statuses)
	var errorMessage strings.Builder

	for _, status := range statuses {
		statusCounts[status.Status] += 1
		if status.Status == libstack.StatusError {
			errorMessage.WriteString(status.Message + "\n")
		}
	}

	switch {
	case statusCounts[libstack.StatusError] > 0:
		return ResourceStatus{Status: libstack.StatusError, Message: errorMessage.String()}
	case statusCounts[libstack.StatusStarting] > 0:
		return ResourceStatus{Status: libstack.StatusStarting}
	case statusCounts[libstack.StatusRemoving] > 0:
		return ResourceStatus{Status: libstack.StatusRemoving}
	case statusCounts[libstack.StatusCompleted] == totalCount:
		return ResourceStatus{Status: libstack.StatusCompleted}
	case statusCounts[libstack.StatusRunning]+statusCounts[libstack.StatusCompleted] == totalCount:
		return ResourceStatus{Status: libstack.StatusRunning}
	case statusCounts[libstack.StatusStopped]+statusCounts[libstack.StatusCompleted] == totalCount:
		return ResourceStatus{Status: libstack.StatusStopped}
	case statusCounts[libstack.StatusRemoved]+statusCounts[libstack.StatusCompleted] == totalCount:
		return ResourceStatus{Status: libstack.StatusRemoved}
	default:
		return ResourceStatus{Status: libstack.StatusUnknown}
	}
}

func podLastTerminationReasonToLibstackStatus(reason string) libstack.Status {
	// we may need to detect other reasons in specific scenarios and match them to libstack Statuses
	switch reason {
	case "Completed":
		return libstack.StatusCompleted
	case "Error":
		return libstack.StatusError
	default:
		return libstack.StatusUnknown
	}
}

func podMessageFromContainerState(status libstack.Status, state corev1.ContainerState) string {
	if status == libstack.StatusCompleted {
		return ""
	}

	if state.Terminated != nil {
		return state.Terminated.Reason + " " + state.Terminated.Message
	}

	if state.Waiting != nil {
		return state.Waiting.Reason + " " + state.Waiting.Message
	}

	return ""
}

// ListNamespaces returns the resourceVersion of every namespace on the
// cluster, keyed by name. resourceVersion changes on every update to a
// namespace object (not just create/delete, e.g. a label edit), which makes
// this suitable for cheap drift detection: callers can diff two snapshots to
// tell whether anything about the namespace topology changed since the last
// check, without re-fetching or re-rendering anything themselves.
func (kcl *KubeClient) ListNamespaces(ctx context.Context) (map[string]string, error) {
	list, err := kcl.cli.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	namespaces := make(map[string]string, len(list.Items))
	for _, ns := range list.Items {
		namespaces[ns.Name] = ns.ResourceVersion
	}
	return namespaces, nil
}

// GetResource retrieves a Kubernetes resource by group/version/resource and name/namespace
func (kcl *KubeClient) GetResource(ctx context.Context, apiVersion, kind, name, namespace string) (any, error) {
	gvr, err := parseGroupVersionResource(apiVersion, kind)
	if err != nil {
		return nil, err
	}

	var resource any
	if namespace == "" {
		// Cluster-scoped resource
		resource, err = kcl.dynamicCli.Resource(*gvr).Get(ctx, name, metav1.GetOptions{})
	} else {
		// Namespace-scoped resource
		resource, err = kcl.dynamicCli.Resource(*gvr).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	}

	if err != nil {
		return nil, err
	}

	return resource, nil
}

// PatchResource applies a JSON merge patch to a Kubernetes resource
func (kcl *KubeClient) PatchResource(ctx context.Context, apiVersion, kind, name, namespace, patch string) error {
	gvr, err := parseGroupVersionResource(apiVersion, kind)
	if err != nil {
		return err
	}

	patchBytes := []byte(patch)

	if namespace == "" {
		// Cluster-scoped resource
		_, err = kcl.dynamicCli.Resource(*gvr).Patch(ctx, name, types.MergePatchType, patchBytes, metav1.PatchOptions{})
	} else {
		// Namespace-scoped resource
		_, err = kcl.dynamicCli.Resource(*gvr).Namespace(namespace).Patch(ctx, name, types.MergePatchType, patchBytes, metav1.PatchOptions{})
	}

	return err
}

// DeleteResource deletes a Kubernetes resource group/version/resource and name/namespace
func (kcl *KubeClient) DeleteResource(ctx context.Context, apiVersion, kind, name, namespace string) error {
	gvr, err := parseGroupVersionResource(apiVersion, kind)
	if err != nil {
		return err
	}

	if namespace == "" {
		// Cluster-scoped resource
		err = kcl.dynamicCli.Resource(*gvr).Delete(ctx, name, metav1.DeleteOptions{})
	} else {
		// Namespace-scoped resource
		err = kcl.dynamicCli.Resource(*gvr).Namespace(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	}

	return err
}

// parseGroupVersionResource converts API version and kind to a GroupVersionResource
// apiVersion can be "v1" (core API) or "group/version" format
func parseGroupVersionResource(apiVersion, kind string) (*schema.GroupVersionResource, error) {
	// This is a simplified implementation. For a more complete one, we'd need to
	// dynamically discover resources from the API server. For now, we support common cases.
	var gvr schema.GroupVersionResource

	if apiVersion == "v1" {
		// Core API group
		gvr = schema.GroupVersionResource{
			Version:  "v1",
			Resource: kindToResource(kind),
		}
	} else {
		// Format: group/version
		parts := strings.Split(apiVersion, "/")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid apiVersion format: %s", apiVersion)
		}
		gvr = schema.GroupVersionResource{
			Group:    parts[0],
			Version:  parts[1],
			Resource: kindToResource(kind),
		}
	}

	return &gvr, nil
}

// kindToResource converts a Kind to its plural resource name
func kindToResource(kind string) string {
	// Simple conversion: lowercase and add 's' for most cases
	// For more complex cases (e.g., "Policy" -> "policies"), this would need a lookup table
	lower := strings.ToLower(kind)
	if strings.HasSuffix(lower, "y") {
		return lower[:len(lower)-1] + "ies"
	}
	return lower + "s"
}

// CRDResourceReady checks whether a CRD's API endpoint is registered in discovery
// by attempting to list resources of that type. crdName is the full CRD name
// (e.g. "k8spspflexvolumes.constraints.gatekeeper.sh").
func (kcl *KubeClient) CRDResourceReady(ctx context.Context, crdName string) bool {
	resource, group, ok := strings.Cut(crdName, ".")
	if !ok {
		return false
	}
	gvr := schema.GroupVersionResource{
		Group:    group,
		Version:  "v1beta1",
		Resource: resource,
	}
	_, err := kcl.dynamicCli.Resource(gvr).List(ctx, metav1.ListOptions{Limit: 1})
	return err == nil
}
