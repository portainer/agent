package kubernetes

import (
	"errors"
	"io"

	"k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	utilexec "k8s.io/client-go/util/exec"
)

// KubeClient can be used to query the Kubernetes API
type KubeClient struct {
	cli *kubernetes.Clientset
}

// NewKubeClient returns a pointer to a new KubeClient instance
func NewKubeClient() (*KubeClient, error) {
	kubeCli := &KubeClient{}

	cli, err := buildLocalClient()
	if err != nil {
		return nil, err
	}

	kubeCli.cli = cli
	return kubeCli, nil
}

func buildLocalClient() (*kubernetes.Clientset, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}

	return kubernetes.NewForConfig(config)
}

// StartExecProcess will start an exec process inside a container located inside a pod inside a specific namespace
// using the specified command. The stdin parameter will be bound to the stdin process and the stdout process will write
// to the stdout parameter.
// This function only works against a local endpoint using an in-cluster config.
func (kcl *KubeClient) StartExecProcess(token, namespace, podName, containerName string, command []string, stdin io.Reader, stdout io.Writer) error {
	config, err := rest.InClusterConfig()
	if err != nil {
		return err
	}

	if token != "" {
		config.BearerToken = token
		config.BearerTokenFile = ""
	}

	req := kcl.cli.CoreV1().RESTClient().
		Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec")

	req.VersionedParams(&v1.PodExecOptions{
		Container: containerName,
		Command:   command,
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
		Stdin:  stdin,
		Stdout: stdout,
		Tty:    true,
	})
	if err != nil {
		if _, ok := err.(utilexec.ExitError); !ok {
			return errors.New("unable to start exec process")
		}
	}

	return nil
}
