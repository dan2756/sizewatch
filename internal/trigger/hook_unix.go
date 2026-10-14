//go:build linux || darwin

package trigger

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// killGroupOnCancel runs cmd in a process group of its own and, when its
// context ends, kills the whole group rather than just sh, so that nothing
// the hook started outlives it.
func killGroupOnCancel(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
}
