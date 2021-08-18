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

// Deploy will deploy a Kubernetes manifest inside the default namespace
// it will use kubectl to deploy the manifest.
// kubectl uses in-cluster config.
func (deployer *KubernetesDeployer) Deploy(ctx context.Context, name string, filePaths []string, prune bool) error {
	if len(filePaths) == 0 {
		return errors.New("missing file paths")
	}

	stackFilePath := filePaths[0]

	args, err := buildArgs(&argOptions{
		Namespace: "default",
	})
	if err != nil {
		return err
	}

	args = append(args, "apply", "-f", stackFilePath)

	_, err = runCommandAndCaptureStdErr(deployer.command, args, nil)
	return err
}

func (deployer *KubernetesDeployer) Remove(ctx context.Context, name string, filePaths []string) error {
	if len(filePaths) == 0 {
		return errors.New("missing file paths")
	}

	stackFilePath := filePaths[0]

	args, err := buildArgs(&argOptions{
		Namespace: "default",
	})
	if err != nil {
		return err
	}

	args = append(args, "delete", "-f", stackFilePath)

	_, err = runCommandAndCaptureStdErr(deployer.command, args, nil)
	return err

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
