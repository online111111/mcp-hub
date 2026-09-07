//go:build windows

package config

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

const windowsLockOffset = 4096

func lockOverlapped() windows.Overlapped {
	return windows.Overlapped{Offset: windowsLockOffset}
}

func tryLockFile(f *os.File) (bool, error) {
	overlapped := lockOverlapped()
	err := windows.LockFileEx(
		windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0,
		1,
		0,
		&overlapped,
	)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) || errors.Is(err, windows.ERROR_IO_PENDING) {
		return false, nil
	}
	return false, err
}

func unlockFile(f *os.File) error {
	overlapped := lockOverlapped()
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &overlapped)
}
