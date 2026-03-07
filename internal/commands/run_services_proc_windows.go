//go:build windows

package commands

import "os/exec"

func setRunServiceProcessGroup(cmd *exec.Cmd) {}

func killRunServiceProcessGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
}
