//go:build unix

package deepsearch

import (
	"os/exec"
	"syscall"
)

// killProcessGroup starts the command in its own process group and makes
// cancelling it kill the whole group, so helper processes yt-dlp starts
// (a JavaScript runtime, ffprobe) do not outlive a cancelled run.
func killProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
}
