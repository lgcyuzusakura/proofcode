//go:build !windows

package main

import (
	"os"
	"path/filepath"
)

func realDesktopDirectory() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Desktop"), nil
}
