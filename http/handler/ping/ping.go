package ping

import (
	"bytes"
	"github.com/portainer/agent"
	httperror "github.com/portainer/libhttp/error"
	"github.com/portainer/libhttp/response"
	"log"
	"net/http"
	osexec "os/exec"
	"path"
)

func (h *Handler) ping(rw http.ResponseWriter, request *http.Request) *httperror.HandlerError {

	// TEST HERE
	log.Printf("[INFO] PING")

	command := path.Join(agent.DockerBinaryPath, "rpc.exe")
	args := []string{"--amtinfo", "all"}
	res := runCommandAndCaptureStdErr(command, args)
	log.Printf("[INFO] result rpc: %s", string(res))

	command = path.Join(agent.DockerBinaryPath, "docker.exe")
	args = []string{"ps"}
	res = runCommandAndCaptureStdErr(command, args)
	log.Printf("[INFO] result docker: %s", string(res))

	return response.Empty(rw)
}

func runCommandAndCaptureStdErr(command string, args []string) []byte {
	var stderr bytes.Buffer
	cmd := osexec.Command(command, args...)
	cmd.Stderr = &stderr

	output, err := cmd.Output()

	if err != nil {
		log.Printf("[ERROR] command %s: %s %s", command, err, stderr.String())
	}

	return output
}
