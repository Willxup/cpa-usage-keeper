package service

import (
	"cpa-usage-keeper/internal/cpa"
	"strings"
)

// CredentialMutationLocks 在同一 CPA 实例的凭证服务间共享，避免启停和优先级操作互相覆盖。
// 零值可用；批量启停、单条启停与优先级服务必须注入同一个实例。
type CredentialMutationLocks struct {
	keyedMutex
}

// 单条入口先解析文件名，批量入口直接使用文件名，以同一上游文件作为锁粒度。
func (l *CredentialMutationLocks) lockCredential(name string) func() {
	return l.lock("credential:" + strings.TrimSpace(name))
}

// 同供应商所有组和 Key 共写一份列表，锁覆盖 GET、定位、PUT 和必要本地更新。
// 按配置路径而非 auth_index 加锁，才能串行化不同 Key 对同一列表的修改。
// 该锁只协调本进程服务，不协调 CPA 管理页面或其他 Keeper 实例的外部写入。
func (l *CredentialMutationLocks) lockProviderConfig(providerType string) func() {
	path, _ := cpa.ProviderConfigPath(providerType)
	return l.lock("provider-config:" + path)
}
