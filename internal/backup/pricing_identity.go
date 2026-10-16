package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// PricingBackupSHA256 绑定完整、静止且无需 sidecar 的 M1 文件；文件副本可搬迁但不可改写。
// 调用方必须停止对该备份的写入；非空 WAL/journal 会使 SQLite 视图不同于主文件，必须拒绝。
func PricingBackupSHA256(ctx context.Context, path string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := checkPricingBackupSidecars(path); err != nil {
		return "", err
	}
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open M1 backup for SHA-256: %w", err)
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil || !before.Mode().IsRegular() {
		return "", fmt.Errorf("M1 backup SHA-256 requires a regular file: %v", err)
	}
	hash := sha256.New()
	buffer := make([]byte, 64*1024)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		count, err := file.Read(buffer)
		if count > 0 {
			_, _ = hash.Write(buffer[:count])
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("read M1 backup for SHA-256: %w", err)
		}
	}
	after, err := os.Stat(path)
	if err != nil || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return "", fmt.Errorf("M1 backup changed while computing SHA-256: %v", err)
	}
	if err := checkPricingBackupSidecars(path); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func checkPricingBackupSidecars(path string) error {
	for _, suffix := range []string{"-wal", "-journal"} {
		info, err := os.Stat(path + suffix)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("check M1 backup sidecar %s: %w", suffix, err)
		}
		if !info.Mode().IsRegular() || info.Size() != 0 {
			return fmt.Errorf("M1 backup has a nonempty SQLite sidecar %s; use the original standalone backup", suffix)
		}
	}
	return nil
}
