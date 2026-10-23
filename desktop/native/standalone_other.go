//go:build !windows

package main

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

type standaloneJob struct{ commands []*exec.Cmd }

func newStandaloneJob() (*standaloneJob, error) { return &standaloneJob{}, nil }
func (job *standaloneJob) attach(command *exec.Cmd) error {
	job.commands = append(job.commands, command)
	return nil
}
func (job *standaloneJob) close() {
	for _, command := range job.commands {
		_ = command.Process.Kill()
	}
}

type standaloneLock struct {
	*os.File
	path string
}

func (lock *standaloneLock) Close() error {
	err := lock.File.Close()
	_ = os.Remove(lock.path)
	return err
}
func lockStandaloneDirectory(directory string) (io.Closer, error) {
	path := filepath.Join(directory, "instance.lock")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	return &standaloneLock{file, path}, nil
}

func protectStandaloneDirectory(directory string) error { return os.Chmod(directory, 0700) }
