package exec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"runtime"
	"strings"

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

// Deploy will deploy a Kubernetes manifest inside a specific namespace
// it will use kubectl to deploy the manifest.
// kubectl uses in-cluster config.
func (deployer *KubernetesDeployer) Deploy(ctx context.Context, name string, filePaths []string, prune bool) error {
	if len(filePaths) == 0 {
		return errors.New("missing file paths")
	}

	stackFilePath := filePaths[0]

	args := make([]string, 0)
	// Specifying "--insecure-skip-tls-verify" make kubectl return error "default cluster has no server defined"
	//args = append(args, "--insecure-skip-tls-verify")
	args = append(args, "--namespace", "default")
	args = append(args, "apply", "-f", stackFilePath)

	var stderr bytes.Buffer
	cmd := exec.Command(deployer.command, args...)
	cmd.Stderr = &stderr

	_, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed deploying kubernetes stack %w: %s", err, stderr.String())
	}

	return nil
}

func (deployer *KubernetesDeployer) Remove(ctx context.Context, name string, filePaths []string) error {
	if len(filePaths) == 0 {
		return errors.New("missing file paths")
	}

	stackFilePath := filePaths[0]

	args := []string{}

	args = append(args, "--namespace", "default")
	args = append(args, "delete", "-f", stackFilePath)

	var stderr bytes.Buffer
	cmd := exec.Command(deployer.command, args...)
	cmd.Stderr = &stderr

	_, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed removing kubernetes stack %w: %s", err, stderr.String())
	}

	return nil
}

// DeployRawConfig will deploy a Kubernetes manifest inside a specific namespace
// it will use kubectl to deploy the manifest and receives a raw config.
// kubectl uses in-cluster config.
func (deployer *KubernetesDeployer) DeployRawConfig(token, config string, namespace string) ([]byte, error) {
	args := make([]string, 0)

	if token != "" {
		host := os.Getenv(agent.KubernetesServiceHost)
		if host == "" {
			return nil, fmt.Errorf("%s env var is not defined", agent.KubernetesServiceHost)
		}

		port := os.Getenv(agent.KubernetesServicePortHttps)
		if port == "" {
			return nil, fmt.Errorf("%s env var is not defined", agent.KubernetesServicePortHttps)
		}

		server := fmt.Sprintf("https://%s:%s", host, port)

		args = append(args, "--token", token)
		args = append(args, "--server", server)
		args = append(args, "--insecure-skip-tls-verify")
	}

	args = append(args, "--namespace", namespace)
	args = append(args, "apply", "-f", "-")

	var stderr bytes.Buffer
	cmd := exec.Command(deployer.command, args...)
	cmd.Stderr = &stderr
	cmd.Stdin = strings.NewReader(config)

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, stderr.String())
	}

	return output, nil
}
