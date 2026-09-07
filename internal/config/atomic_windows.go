//go:build windows

package config

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modkernel32     = windows.NewLazySystemDLL("kernel32.dll")
	procReplaceFile = modkernel32.NewProc("ReplaceFileW")
)

const (
	replaceFileIgnoreMergeErrors = 0x00000002
)

func replaceFile(from, to string) error {
	fromUTF16, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	toUTF16, err := windows.UTF16PtrFromString(to)
	if err != nil {
		return err
	}

	// Try ReplaceFileW first (replaces an existing file atomically while preserving metadata).
	// lpReplacedFileName is 'to', lpReplacementFileName is 'from'.
	r1, _, callErr := procReplaceFile.Call(
		uintptr(unsafe.Pointer(toUTF16)),
		uintptr(unsafe.Pointer(fromUTF16)),
		0,
		uintptr(replaceFileIgnoreMergeErrors),
		0,
		0,
	)
	if r1 != 0 {
		return nil
	}

	// If ReplaceFile failed because the destination file does not exist, use MoveFileEx.
	if errors.Is(callErr, windows.ERROR_FILE_NOT_FOUND) || errors.Is(callErr, windows.ERROR_PATH_NOT_FOUND) {
		return windows.MoveFileEx(fromUTF16, toUTF16, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
	}

	return callErr
}

func syncParentDir(string) error { return nil }
