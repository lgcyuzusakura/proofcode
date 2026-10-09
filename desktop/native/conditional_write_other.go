//go:build !windows

package main

import "errors"

func conditionalSourceWrite(_ string, _ string, _ sourceFile, _ bool, _ sourceFile, _ bool) error {
	return errors.New("automatic patch application requires the Windows exclusive file adapter; review and merge the patch manually")
}
