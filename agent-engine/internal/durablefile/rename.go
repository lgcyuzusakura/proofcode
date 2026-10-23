package durablefile

import (
	"errors"
	"os"
	"runtime"
	"syscall"
	"time"
)

// Rename preserves os.Rename's failure semantics, with a bounded retry for
// transient Windows file locks. Never delete the destination to force a replace.
func Rename(source, target string) error {
	deadline := time.Now().Add(time.Second)
	delay := 5 * time.Millisecond
	for {
		err := os.Rename(source, target)
		if err == nil || runtime.GOOS != "windows" {
			return err
		}
		var code syscall.Errno
		if !errors.As(err, &code) || (code != 5 && code != 32 && code != 33) {
			return err
		} // Access denied, sharing violation, lock violation.
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return err
		}
		pause := delay
		if pause > remaining {
			pause = remaining
		}
		time.Sleep(pause)
		if delay < 80*time.Millisecond {
			delay *= 2
		}
	}
}
