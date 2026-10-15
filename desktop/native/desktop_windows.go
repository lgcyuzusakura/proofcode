//go:build windows

package main

import "golang.org/x/sys/windows"

func realDesktopDirectory() (string, error) {
	return windows.KnownFolderPath(windows.FOLDERID_Desktop, windows.KF_FLAG_DEFAULT)
}
