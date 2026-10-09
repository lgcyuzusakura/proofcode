//go:build windows

package main

import (
	"os/exec"
	"strconv"
)

func stopCommandTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return exec.Command("taskkill", "/PID", strconv.Itoa(cmd.Process.Pid), "/T", "/F").Run()
}
