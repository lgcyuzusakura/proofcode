package durablefile

import (
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRenameWaitsForActualWindowsHandleAndPreservesPermanentlyLockedState(t *testing.T) {
	for _, release := range []bool{true, false} {
		name := "permanent"
		if release {
			name = "temporary"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			source, target := filepath.Join(dir, "next"), filepath.Join(dir, "saved")
			if err := os.WriteFile(source, []byte("new state"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(target, []byte("old state"), 0600); err != nil {
				t.Fatal(err)
			}
			path, err := windows.UTF16PtrFromString(target)
			if err != nil {
				t.Fatal(err)
			}
			handle, err := windows.CreateFile(path, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(source, target); err == nil {
				windows.CloseHandle(handle)
				t.Fatal("fixture did not lock destination")
			}
			if release {
				done := make(chan struct{})
				go func() { defer close(done); time.Sleep(50 * time.Millisecond); windows.CloseHandle(handle) }()
				err = Rename(source, target)
				<-done
				if err != nil {
					t.Fatal(err)
				}
				if data, err := os.ReadFile(target); err != nil || string(data) != "new state" {
					t.Fatal("released checkpoint did not save")
				}
			} else {
				err = Rename(source, target)
				windows.CloseHandle(handle)
				if err == nil {
					t.Fatal("permanently locked replacement succeeded")
				}
				if data, err := os.ReadFile(target); err != nil || string(data) != "old state" {
					t.Fatal("locked checkpoint was damaged")
				}
				if data, err := os.ReadFile(source); err != nil || string(data) != "new state" {
					t.Fatal("failed save lost recovery candidate")
				}
			}
		})
	}
}
