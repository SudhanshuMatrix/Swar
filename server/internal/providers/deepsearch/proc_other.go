//go:build !unix

package deepsearch

import "os/exec"

// killProcessGroup is a no-op where Go has no process groups (Windows):
// cancelling the command kills the yt-dlp process itself. A helper process
// it started may outlive it until it notices its parent is gone.
func killProcessGroup(cmd *exec.Cmd) {}
