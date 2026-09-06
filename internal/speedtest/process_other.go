//go:build !linux

package speedtest

import (
	"os"
	"os/exec"
)

func configureProcess(cmd *exec.Cmd) {}

func terminateProcess(process *os.Process) error {
	if process == nil {
		return nil
	}

	return process.Kill()
}
