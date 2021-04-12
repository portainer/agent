package exec

import (
	"bytes"
	"fmt"
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
// kubectl uses in-cluster config.
func (deployer *KubernetesDeployer) Deploy(data string, namespace string) ([]byte, error) {
	command := path.Join(deployer.binaryPath, "kubectl")
	if runtime.GOOS == "windows" {
		command = path.Join(deployer.binaryPath, "kubectl.exe")
	}

	args := make([]string, 0)
	// Specifying "--insecure-skip-tls-verify" make kubectl return error "default cluster has no server defined"
	//args = append(args, "--insecure-skip-tls-verify")
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
