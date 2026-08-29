//go:build linux

package capacity

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

type FileLock struct {
	file *os.File
}

func AcquireFileLock(path string) (*FileLock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create benchmark lock directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open benchmark lock: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, fmt.Errorf("another benchmark owns %s: %w", filepath.Clean(path), err)
	}
	return &FileLock{file: file}, nil
}

func (lock *FileLock) Close() error {
	if lock == nil || lock.file == nil {
		return nil
	}
	err := syscall.Flock(int(lock.file.Fd()), syscall.LOCK_UN)
	err = errors.Join(err, lock.file.Close())
	lock.file = nil
	return err
}
