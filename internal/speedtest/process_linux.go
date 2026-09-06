//go:build linux

package speedtest

import (
	"os"
	"os/exec"
	"syscall"
)

func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid:   true,
		Pdeathsig: syscall.SIGKILL,
	}
}

func terminateProcess(process *os.Process) error {
	if process == nil {
		return nil
	}

	if process.Pid > 0 {
		_ = syscall.Kill(-process.Pid, syscall.SIGKILL)
	}

	return process.Kill()
}

func (p *execProcess) OwnsTCPListener(address string) (bool, error) {
	if p == nil || p.pid <= 0 {
		return false, nil
	}
	return processOwnsTCPListenerAt("/proc", p.pid, address)
}
