//go:build !windows

package runtime

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// Docker 会派生 buildx；取消整个专属进程组，避免子进程持有管道导致 Wait 一直不返回。
func boundCommandLifetime(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = 2 * time.Second
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
}
