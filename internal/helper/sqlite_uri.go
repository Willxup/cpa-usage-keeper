package helper

import (
	"net/url"
	"path/filepath"
)

// BuildSQLiteFileURI 把已经绝对化的本地文件名转换成 SQLite file URI，并保留跨平台路径语义。
func BuildSQLiteFileURI(filename string) string {
	// Windows 盘符必须位于 URI path 的 /C:/... 中；缺少前导斜杠会被 net/url 误写成 authority。
	uriPath := filepath.ToSlash(filename)
	if len(uriPath) >= 2 && uriPath[1] == ':' && ((uriPath[0] >= 'A' && uriPath[0] <= 'Z') || (uriPath[0] >= 'a' && uriPath[0] <= 'z')) {
		uriPath = "/" + uriPath
	}
	// url.URL 继续负责空格、# 等字符的标准转义；Unix 绝对路径和 UNC 路径保持原样。
	return (&url.URL{Scheme: "file", Path: uriPath}).String()
}
