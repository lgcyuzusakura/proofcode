//go:build !windows

package runtimecheck

import "os/exec"

func configureCommand(command *exec.Cmd) {}
