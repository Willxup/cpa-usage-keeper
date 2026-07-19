package service

import (
	"context"

	"cpa-usage-keeper/internal/cpa/dto/response"
	"cpa-usage-keeper/internal/service/providermetadata"
)

// MetadataFetcher 汇总 Auth Files、管理 API Keys 与七个 provider endpoint 的只读依赖。
type MetadataFetcher interface {
	// FetchAuthFiles 读取 CPA OAuth/Auth File metadata。
	FetchAuthFiles(context.Context) (*response.AuthFilesResult, error)
	// FetchManagementAPIKeys 读取 CPA 管理接口访问 key 清单。
	FetchManagementAPIKeys(context.Context) (*response.ManagementAPIKeysResult, error)
	// Fetcher 嵌入 provider 纯包定义的固定七来源接口，避免 service 再维护第二份列表。
	providermetadata.Fetcher
}

// CPAClientFetcher 保留 CPA client 的聚合接口边界，构造器和 validate 继续使用原有类型。
type CPAClientFetcher interface {
	// MetadataFetcher 表示真实 CPA client 必须满足完整 metadata 合同。
	MetadataFetcher
}
