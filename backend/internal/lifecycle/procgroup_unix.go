//go:build unix

package lifecycle

import (
	"os/exec"
	"syscall"
	"time"
)

// killWaitDelay bounds how long Wait keeps reading pipes after a kill.
const killWaitDelay = 5 * time.Second

// killProcessGroupOnCancel starts cmd in its own process group and makes
// context cancellation kill that group, so no child Compose spawned outlives
// the run.
func killProcessGroupOnCancel(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = killWaitDelay
}
