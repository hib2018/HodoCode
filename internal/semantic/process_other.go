//go:build !darwin && !linux && !freebsd && !openbsd && !netbsd && !dragonfly

package semantic

import "os/exec"

// shortcut: only the direct server is killed here; add platform job control
// before using servers that spawn descendants on these operating systems.
func setupProcessGroup(cmd *exec.Cmd) {}

func killProcessGroup(cmd *exec.Cmd) {
	_ = cmd.Process.Kill()
}
