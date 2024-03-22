package exec

import (
	"context"
	"fmt"
	"os"
	"path"
	"runtime"

	"github.com/pkg/errors"
	"github.com/portainer/agent"
)

// KubernetesDeployer represents a service to deploy resources inside a Kubernetes environment.
type KubernetesDeployer struct {
	command string
}

// NewKubernetesDeployer initializes a new KubernetesDeployer service.
func NewKubernetesDeployer(binaryPath string) *KubernetesDeployer {
	command := path.Join(binaryPath, "kubectl")
	if runtime.GOOS == "windows" {
		command = path.Join(binaryPath, "kubectl.exe")
	}

	return &KubernetesDeployer{
		command: command,
	}
}

func (deployer *KubernetesDeployer) operation(ctx context.Context, name string, filePaths []string, operation, namespace string) error {
	if len(filePaths) == 0 {
		return errors.New("missing file paths")
	}

	stackFilePath := filePaths[0]

	args, err := buildArgs(&argOptions{
		Namespace: namespace,
	})
	if err != nil {
		return err
	}

	args = append(args, operation, "-f", stackFilePath)

	_, err = runCommandAndCaptureStdErr(deployer.command, args, nil)
	return err
}

// Deploy will deploy a Kubernetes manifest inside the default namespace
// it will use kubectl to deploy the manifest.
// kubectl uses in-cluster config.
func (deployer *KubernetesDeployer) Deploy(ctx context.Context, name string, filePaths []string, options agent.DeployOptions) error {
	return deployer.operation(ctx, name, filePaths, "apply", options.Namespace)
}

func (deployer *KubernetesDeployer) Remove(ctx context.Context, name string, filePaths []string, options agent.RemoveOptions) error {
	return deployer.operation(ctx, name, filePaths, "delete", options.Namespace)
}

// Pull is a dummy method for Kube
func (deployer *KubernetesDeployer) Pull(ctx context.Context, name string, filePaths []string, options agent.PullOptions) error {
	return nil
}

// Validate is a dummy method for Kubernetes manifest validation
// https://portainer.atlassian.net/browse/EE-6292?focusedCommentId=29674
func (deployer *KubernetesDeployer) Validate(ctx context.Context, name string, filePaths []string, options agent.ValidateOptions) error {
	return nil
}

// DeployRawConfig will deploy a Kubernetes manifest inside a specific namespace
// it will use kubectl to deploy the manifest and receives a raw config.
// kubectl uses in-cluster config.
func (deployer *KubernetesDeployer) DeployRawConfig(token, config string, namespace string) ([]byte, error) {
	args, err := buildArgs(&argOptions{
		Namespace: namespace,
		Token:     token,
	})
	if err != nil {
		return nil, err
	}

	args = append(args, "apply", "-f", "-")

	return runCommandAndCaptureStdErr(deployer.command, args, &cmdOpts{Input: config})
}

type argOptions struct {
	Namespace string
	Token     string
}

func buildArgs(opts *argOptions) ([]string, error) {
	args := []string{}

	if opts == nil {
		return args, nil
	}

	if opts.Namespace != "" {
		args = append(args, "--namespace", opts.Namespace)
	}

	if opts.Token != "" {
		tokenArgs, err := buildTokenArgs(opts.Token)
		if err != nil {
			return nil, errors.Wrap(err, "failed building token args")
		}

		args = append(args, tokenArgs...)
	}

	return args, nil
}

func buildTokenArgs(token string) ([]string, error) {
	host := os.Getenv(agent.KubernetesServiceHost)
	if host == "" {
		return nil, fmt.Errorf("%s env var is not defined", agent.KubernetesServiceHost)
	}

	port := os.Getenv(agent.KubernetesServicePortHttps)
	if port == "" {
		return nil, fmt.Errorf("%s env var is not defined", agent.KubernetesServicePortHttps)
	}

	server := fmt.Sprintf("https://%s:%s", host, port)

	return []string{
		"--token", token,
		"--server", server,
		"--insecure-skip-tls-verify",
	}, nil

}
