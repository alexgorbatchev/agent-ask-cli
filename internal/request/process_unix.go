//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package request

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func ownProcess(cmd *exec.Cmd) (*processOwner, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	kill := func() error {
		if cmd.Process == nil {
			return nil
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.Cancel = kill
	return &processOwner{afterStart: func() error { return nil }, kill: kill, close: func() error { return nil }}, nil
}
