//go:build windows

package repository

import (
	"fmt"

	"golang.org/x/sys/windows"
)

func storageAvailableDiskBytes(path string) (uint64, error) {
	directoryName, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, fmt.Errorf("encode disk path %s: %w", path, err)
	}
	var available uint64
	if err := windows.GetDiskFreeSpaceEx(directoryName, &available, nil, nil); err != nil {
		return 0, fmt.Errorf("load available disk space for %s: %w", path, err)
	}
	return available, nil
}
