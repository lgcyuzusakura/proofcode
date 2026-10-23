//go:build windows

package main

import (
	"os/exec"
	"strings"
	"syscall"
)

func configureRuntimeCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
func runtimePathEqual(first, second string) bool { return strings.EqualFold(first, second) }
