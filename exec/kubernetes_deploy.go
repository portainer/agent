package exec

import (
	"bytes"
	"fmt"
	"io/ioutil"
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
func (deployer *KubernetesDeployer) Deploy(data string, namespace string) ([]byte, error) {

	token, err := ioutil.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/token")
	if err != nil {
		return nil, err
	}

	command := path.Join(deployer.binaryPath, "kubectl")
	if runtime.GOOS == "windows" {
		command = path.Join(deployer.binaryPath, "kubectl.exe")
	}

	args := make([]string, 0)
	args = append(args, "--insecure-skip-tls-verify")
	args = append(args, "--token", string(token))
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
