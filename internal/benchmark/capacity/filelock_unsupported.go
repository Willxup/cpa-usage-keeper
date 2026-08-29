//go:build !linux

package capacity

import "fmt"

type FileLock struct{}

func AcquireFileLock(path string) (*FileLock, error) {
	return nil, fmt.Errorf("benchmark file locking is unavailable for %s: capacity benchmark requires linux/amd64", path)
}

func (lock *FileLock) Close() error {
	return nil
}
