package service

import "strings"

// CredentialMutationLocks 在同一 CPA 实例的凭证服务间共享，避免不同操作覆盖同一认证文件。
// 零值可用；批量启停、单条启停与优先级服务必须注入同一个实例。
type CredentialMutationLocks struct {
	keyedMutex
}

// 单条入口先解析文件名，批量入口直接使用文件名，以同一上游文件作为锁粒度。
func (l *CredentialMutationLocks) lockAuthFile(name string) func() {
	return l.lock("auth-file:" + strings.TrimSpace(name))
}
