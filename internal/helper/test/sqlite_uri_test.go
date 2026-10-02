package test

import (
	"testing"

	"cpa-usage-keeper/internal/helper"
)

func TestBuildSQLiteFileURIKeepsWindowsDriveInsideURIPath(t *testing.T) {
	// 准备：传入 filepath.ToSlash 在 Windows 上产生的盘符路径，并包含必须转义的特殊字符。
	filename := "C:/data/app #reader.db"

	// 执行：统一 URI helper 必须把盘符保留在 path，而不能让 net/url 把 C: 解释成 authority。
	got := helper.BuildSQLiteFileURI(filename)

	// 断言：SQLite 官方支持的本地盘符形式固定为 file:///C:/...，且文件名经过 URI 转义。
	const want = "file:///C:/data/app%20%23reader.db"
	if got != want {
		t.Fatalf("expected Windows drive URI %q, got %q", want, got)
	}
}
