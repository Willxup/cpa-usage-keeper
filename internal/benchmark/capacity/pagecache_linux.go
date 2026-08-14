//go:build linux

package capacity

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func DropFilePageCache(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := unix.Fadvise(int(file.Fd()), 0, 0, unix.FADV_DONTNEED); err != nil {
		return fmt.Errorf("fadvise DONTNEED %s: %w", path, err)
	}
	return nil
}
