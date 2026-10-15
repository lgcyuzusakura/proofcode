//go:build windows

package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// Hold a Windows exclusive file handle across compare and write. Existing
// editor handles or new saves/replacements cause sharing conflicts, not lost edits.
func conditionalSourceWrite(root, relative string, expected sourceFile, exists bool, next sourceFile, present bool) error {
	path, err := safeTarget(root, relative)
	if err != nil {
		return err
	}
	if !exists && !present {
		if _, err = os.Lstat(path); os.IsNotExist(err) {
			return nil
		}
		return errors.New("absent patch target now exists")
	}
	var data []byte
	if present {
		data, err = base64.StdEncoding.DecodeString(next.Content)
		if err != nil {
			return err
		}
	}
	if !exists {
		if err = os.MkdirAll(filepath.Dir(path), 0750); err != nil {
			return err
		}
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	creation := uint32(windows.OPEN_EXISTING)
	if !exists {
		creation = windows.CREATE_NEW
	}
	access := uint32(windows.GENERIC_READ | windows.GENERIC_WRITE)
	if !present {
		access |= windows.DELETE
	}
	handle, err := windows.CreateFile(name, access, 0, nil, creation, windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return fmt.Errorf("patch target %s is changed or busy: %w", relative, err)
	}
	file := os.NewFile(uintptr(handle), path)
	defer file.Close()
	var info windows.ByHandleFileInformation
	if err = windows.GetFileInformationByHandle(handle, &info); err != nil {
		return err
	}
	if info.FileAttributes&(windows.FILE_ATTRIBUTE_REPARSE_POINT|windows.FILE_ATTRIBUTE_DIRECTORY) != 0 || info.NumberOfLinks != 1 {
		return errors.New("patch target must be a regular file without links")
	}
	buffer := make([]uint16, 32768)
	length, err := windows.GetFinalPathNameByHandle(handle, &buffer[0], uint32(len(buffer)), 0)
	if err != nil || length >= uint32(len(buffer)) {
		return errors.New("cannot verify the opened patch target")
	}
	final := strings.TrimPrefix(windows.UTF16ToString(buffer[:length]), `\\?\`)
	if strings.HasPrefix(final, `UNC\`) {
		final = `\\` + strings.TrimPrefix(final, `UNC\`)
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	resolvedRelative, err := filepath.Rel(resolvedRoot, final)
	if err != nil || resolvedRelative == ".." || strings.HasPrefix(resolvedRelative, ".."+string(filepath.Separator)) || !strings.EqualFold(resolvedRelative, filepath.FromSlash(relative)) {
		return errors.New("opened patch target escapes its registered path")
	}
	if exists {
		previous, err := io.ReadAll(io.LimitReader(file, 1<<20+1))
		if err != nil {
			return err
		}
		hash := sha256.Sum256(previous)
		if len(previous) > 1<<20 || hex.EncodeToString(hash[:]) != expected.SHA256 {
			return errors.New("patch target changed before the exclusive write")
		}
	}
	if !present {
		deleteFlag := byte(1)
		return windows.SetFileInformationByHandle(handle, windows.FileDispositionInfo, &deleteFlag, 1)
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if _, err = file.Write(data); err != nil {
		return err
	}
	if err = file.Truncate(int64(len(data))); err != nil {
		return err
	}
	return file.Sync()
}
