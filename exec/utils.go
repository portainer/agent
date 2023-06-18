package exec

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type cmdOpts struct {
	WorkingDir string
	Input      string
	Env        []string
}

func runCommandAndCaptureStdErr(command string, args []string, opts *cmdOpts) ([]byte, error) {
	var stderr bytes.Buffer
	cmd := exec.Command(command, args...)
	cmd.Stderr = &stderr

	if opts != nil {
		if opts.Input != "" {
			cmd.Stdin = strings.NewReader(opts.Input)
		}

		if opts.WorkingDir != "" {
			cmd.Dir = opts.WorkingDir
		}

		if opts.Env != nil {
			cmd.Env = os.Environ()
			cmd.Env = append(cmd.Env, opts.Env...)
		}

	}

	output, err := cmd.Output()

	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, stderr.String())
	}

	return output, nil
}
