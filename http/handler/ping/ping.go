package ping

import (
	"bytes"
	"fmt"
	"github.com/portainer/agent"
	httperror "github.com/portainer/libhttp/error"
	"github.com/portainer/libhttp/response"
	"log"
	"net/http"
	"os/exec"
	"path"
)

func (h *Handler) ping(rw http.ResponseWriter, request *http.Request) *httperror.HandlerError {

	// TEST HERE
	log.Printf("[INFO] PING")
	command := path.Join(agent.DockerBinaryPath, "rpc.exe")
	args := []string{"--amtinfo", "all"}

	res, err := runCommandAndCaptureStdErr(command, args)
	log.Printf("[INFO] result: %s", string(res))
	log.Printf("[INFO] error: %s", err)

	return response.Empty(rw)
}

func runCommandAndCaptureStdErr(command string, args []string) ([]byte, error) {
	var stderr bytes.Buffer
	cmd := exec.Command(command, args...)
	cmd.Stderr = &stderr

	output, err := cmd.Output()

	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, stderr.String())
	}

	return output, nil
}
