package exec

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/pkg/errors"
	"github.com/portainer/agent"
	"github.com/portainer/agent/deployer"
	"github.com/portainer/agent/kubernetes"
	"github.com/portainer/portainer/pkg/libkubectl"
)

const defaultServiceAccountTokenFile = "/var/run/secrets/kubernetes.io/serviceaccount/token"

var (
	_ deployer.Deployer = &KubernetesDeployer{}

	serviceAccountTokenFile = defaultServiceAccountTokenFile
)

// KubernetesDeployer represents a service to deploy resources inside a Kubernetes environment.
type KubernetesDeployer struct {
	kubeClient   *kubernetes.KubeClient
	helmDeployer *HelmDeployer
}

// NewKubernetesDeployer initializes a new KubernetesDeployer service.
func NewKubernetesDeployer(kubeClient *kubernetes.KubeClient) *KubernetesDeployer {
	helmDeployer := NewHelmDeployer(kubeClient)
	return &KubernetesDeployer{
		kubeClient:   kubeClient,
		helmDeployer: helmDeployer,
	}
}

// isHelmDeployment checks if the deployment is a Helm deployment
// by looking for the HELM_CHART_PATH (git-based) or HELM_REPO_URL (repo-based) environment variable
func isHelmDeployment(env []string) bool {
	for _, e := range env {
		if strings.HasPrefix(e, "HELM_CHART_PATH=") || strings.HasPrefix(e, "HELM_REPO_URL=") {
			return true
		}
	}
	return false
}

func (deployer *KubernetesDeployer) operation(_ context.Context, _ string, manifests []string, operation, namespace string) error {
	if len(manifests) == 0 {
		return errors.New("missing manifests")
	}

	var client *libkubectl.Client
	var err error

	// Developers can set the DEV_KUBECONFIG_PATH to run agent locally
	devKubeConfigPath := os.Getenv("DEV_KUBECONFIG_PATH")
	if devKubeConfigPath != "" {
		client, err = libkubectl.NewClient(&libkubectl.ClientAccess{}, namespace, devKubeConfigPath, false)
		if err != nil {
			return errors.Wrap(err, "failed to create kubectl client with kubeconfig")
		}
	} else {
		token, err := os.ReadFile(serviceAccountTokenFile)
		if err != nil {
			return errors.Wrap(err, "failed to read service account token")
		}

		clientAccess, insecure := libkubectl.NewClientAccess(libkubectl.InClusterServerURL, string(token))
		client, err = libkubectl.NewClient(clientAccess, namespace, "", insecure)
		if err != nil {
			return errors.Wrap(err, "failed to create kubectl client")
		}
	}

	operations := map[string]func(context.Context, []string) (string, error){
		"apply":           client.ApplyDynamic,
		"delete":          client.DeleteDynamic,
		"rollout-restart": client.RolloutRestart,
	}

	operationFunc, ok := operations[operation]
	if !ok {
		return errors.Errorf("unsupported operation: %s", operation)
	}

	output, err := operationFunc(context.Background(), manifests)
	if err != nil {
		return errors.Wrapf(err, "failed to execute kubectl %s command", operation)
	}

	fmt.Println(output)

	return nil
}

// Deploy will deploy a Kubernetes manifest inside the default namespace
// it will use kubectl to deploy the manifest.
// kubectl uses in-cluster config.
func (deployer *KubernetesDeployer) Deploy(ctx context.Context, name string, manifests []string, options deployer.DeployOptions) error {
	if isHelmDeployment(options.Env) {
		return deployer.helmDeployer.Deploy(ctx, name, manifests, options)
	}
	return deployer.operation(ctx, name, manifests, "apply", options.Namespace)
}

func (deployer *KubernetesDeployer) Remove(ctx context.Context, name string, manifests []string, options deployer.RemoveOptions) error {
	if isHelmDeployment(options.Env) {
		return deployer.helmDeployer.Remove(ctx, name, manifests, options)
	}
	return deployer.operation(ctx, name, manifests, "delete", options.Namespace)
}

// Pull is a dummy method for Kube
func (deployer *KubernetesDeployer) Pull(ctx context.Context, name string, manifests []string, options deployer.PullOptions) error {
	if isHelmDeployment(options.Env) {
		return deployer.helmDeployer.Pull(ctx, name, manifests, options)
	}
	return nil
}

// Validate is a dummy method for Kubernetes manifest validation
// https://portainer.atlassian.net/browse/EE-6292?focusedCommentId=29674
func (deployer *KubernetesDeployer) Validate(ctx context.Context, name string, manifests []string, options deployer.ValidateOptions) error {
	if isHelmDeployment(options.Env) {
		return deployer.helmDeployer.Validate(ctx, name, manifests, options)
	}
	return nil
}

// DeployRawConfig will deploy a Kubernetes manifest inside a specific namespace
// it will use kubectl to deploy the manifest and receives a raw config.
// kubectl uses in-cluster config.
func (deployer *KubernetesDeployer) DeployRawConfig(token, config string, namespace string) ([]byte, error) {
	host := os.Getenv(agent.KubernetesServiceHost)
	if host == "" {
		return nil, fmt.Errorf("%s env var is not defined", agent.KubernetesServiceHost)
	}

	port := os.Getenv(agent.KubernetesServicePortHttps)
	if port == "" {
		return nil, fmt.Errorf("%s env var is not defined", agent.KubernetesServicePortHttps)
	}

	server := fmt.Sprintf("https://%s:%s", host, port)
	client, err := libkubectl.NewClient(&libkubectl.ClientAccess{
		Token:     token,
		ServerUrl: server,
		CAFile:    libkubectl.InClusterCAFile,
	}, namespace, "", false)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create kubectl client")
	}

	_, err = client.ApplyDynamic(context.Background(), []string{config})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to execute kubectl apply command")
	}

	return nil, nil
}

func (service *KubernetesDeployer) GetEdgeStacks(ctx context.Context) ([]agent.EdgeStack, error) {
	return nil, nil
}
