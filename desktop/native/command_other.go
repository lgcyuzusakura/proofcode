//go:build !windows

package main

import "os/exec"

func stopCommandTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
