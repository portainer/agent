package client

import (
	"bytes"
	"io"
	"sync"

	"github.com/portainer/agent/docker"
)

type LiveLogCollector struct {
	mu         sync.Mutex
	stdOutBuf  *bytes.Buffer
	stdErrBuf  *bytes.Buffer
	stdOutDone bool
	stdErrDone bool
}

func StartNewLiveLogCollector(containerName, since, until, tail string) (*LiveLogCollector, error) {
	stdOutRd, stdErrRd, err := docker.GetLiveContainerLogs(containerName, since, until, tail)
	if err != nil {
		return nil, err
	}

	c := &LiveLogCollector{
		stdOutBuf: &bytes.Buffer{},
		stdErrBuf: &bytes.Buffer{},
	}

	go c.loop(stdOutRd, c.stdOutBuf, &c.stdOutDone)
	go c.loop(stdErrRd, c.stdErrBuf, &c.stdErrDone)

	return c, nil
}

func (c *LiveLogCollector) loop(rd *io.PipeReader, wr *bytes.Buffer, done *bool) {
	buf := make([]byte, 1024)

	for {
		n, err := rd.Read(buf)
		if n > 0 {
			c.mu.Lock()
			_, _ = wr.Write(buf[:n])
			c.mu.Unlock()
		}

		if err != nil {
			c.mu.Lock()
			*done = true
			c.mu.Unlock()

			return
		}
	}
}

func (c *LiveLogCollector) Collect() ([]byte, []byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	defer c.stdOutBuf.Reset()
	defer c.stdErrBuf.Reset()

	return bytes.Clone(c.stdOutBuf.Bytes()),
		bytes.Clone(c.stdErrBuf.Bytes()),
		c.stdOutDone && c.stdErrDone
}
