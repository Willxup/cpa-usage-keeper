//go:build !linux

package capacity

func DropFilePageCache(string) error {
	return nil
}
