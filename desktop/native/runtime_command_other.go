//go:build !windows

package main

import "os/exec"

func configureRuntimeCommand(cmd *exec.Cmd)      {}
func runtimePathEqual(first, second string) bool { return first == second }
