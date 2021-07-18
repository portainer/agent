package exec

import (
	"bytes"
	"fmt"
	"github.com/portainer/agent"
	"os"
	"os/exec"
	"path"
	"runtime"
	"strings"
)

// KubernetesDeployer represents a service to deploy resources inside a Kubernetes environment.
type KubernetesDeployer struct {
	binaryPath string
}

// NewKubernetesDeployer initializes a new KubernetesDeployer service.
func NewKubernetesDeployer(binaryPath string) *KubernetesDeployer {
	return &KubernetesDeployer{
		binaryPath: binaryPath,
	}
}

// Deploy will deploy a Kubernetes manifest inside a specific namespace
// it will use kubectl to deploy the manifest.
func (deployer *KubernetesDeployer) Deploy(token, data string, namespace string) ([]byte, error) {
	command := path.Join(deployer.binaryPath, "kubectl")
	if runtime.GOOS == "windows" {
		command = path.Join(deployer.binaryPath, "kubectl.exe")
	}

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
	cmd := exec.Command(command, args...)
	cmd.Stderr = &stderr
	cmd.Stdin = strings.NewReader(data)

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, stderr.String())
	}

	return output, nil
}
