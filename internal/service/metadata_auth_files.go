package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cpa-usage-keeper/internal/cpa/dto/authfiles"
	"cpa-usage-keeper/internal/cpa/dto/response"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"gorm.io/gorm"
)

// syncAuthFiles 将 CPA auth_files 映射为 OAuth 类 usage identities，并按 auth_type 整体替换。
func syncAuthFiles(ctx context.Context, db *gorm.DB, result *response.AuthFilesResult, fetchErr error, now time.Time) error {
	// fetch failure 没有完整新列表，必须保留所有本地 Auth File identities。
	if fetchErr != nil {
		// 保持原有来源错误包装。
		return fmt.Errorf("fetch auth files: %w", fetchErr)
	}
	// nil 数据库仍返回与旧实现一致的配置错误。
	if db == nil {
		// 不尝试映射或写入。
		return fmt.Errorf("database is nil")
	}
	// nil response 不是成功空列表，不能把全部 Auth Files 标记 stale。
	if result == nil {
		// 保持原有 nil response 错误文本。
		return fmt.Errorf("fetch auth files: empty response")
	}
	// identities 按 CPA Auth Files 原始顺序预分配。
	identities := make([]entities.UsageIdentity, 0, len(result.Payload.Files))
	// 每个 Auth File 都通过原映射和类型扩展生成 OAuth identity。
	for _, file := range result.Payload.Files {
		// 按输入顺序加入整体 replace 清单。
		identities = append(identities, authFileUsageIdentity(file))
	}
	// Auth Files 成功空列表也进入整个 auth type replace，保持旧 stale 语义。
	if err := repository.ReplaceUsageIdentitiesForAuthType(ctx, db, identities, entities.UsageIdentityAuthTypeAuthFile, now); err != nil {
		// repository failure 使用原 service 上下文包装。
		return fmt.Errorf("sync auth file usage identities: %w", err)
	}
	// 本轮 Auth Files 写入完成。
	return nil
}

// authFileUsageIdentityExtension 为已有 Codex 与 xAI Auth File 追加专属字段，不改变通用映射。
type authFileUsageIdentityExtension func(authfiles.AuthFile, *entities.UsageIdentity)

// authFileUsageIdentityExtensions 按规范化 Auth File type 选择既有专属解析逻辑。
var authFileUsageIdentityExtensions = map[string]authFileUsageIdentityExtension{
	// Codex 解析 ChatGPT id_token 的账户、窗口和套餐字段。
	"codex": extendCodexAuthFileUsageIdentity,
	// xAI 解析 CPA claims 中的稳定 user id 候选。
	"xai": extendXAIAuthFileUsageIdentity,
}

// authFileUsageIdentity 先走通用身份映射，再按 type 追加既有专属字段。
func authFileUsageIdentity(file authfiles.AuthFile) entities.UsageIdentity {
	// identity 从所有 Auth Files 共用字段开始构造。
	identity := baseAuthFileUsageIdentity(file)
	// type 只用于选择专属字段解析，原始 Type 字段仍保存在 identity。
	if extend, ok := authFileUsageIdentityExtensions[strings.ToLower(strings.TrimSpace(file.Type))]; ok {
		// 专属扩展只修改自己负责的 nullable 字段。
		extend(file, &identity)
	}
	// ProjectID 继续按既有 Gemini 家族规则解析，unsupported type 返回 nil。
	identity.ProjectID = resolveAuthFileProjectID(file)
	// 返回完整 OAuth identity 供 repository replace。
	return identity
}

// baseAuthFileUsageIdentity 写入所有 auth_files 共享字段，专属字段由扩展函数补充。
func baseAuthFileUsageIdentity(file authfiles.AuthFile) entities.UsageIdentity {
	// 返回不含 Keeper-only alias 和统计字段的上游 metadata 输入。
	return entities.UsageIdentity{
		// Name 按 email、label、name、auth-index 保持原 fallback 顺序。
		Name: firstNonEmpty(file.Email, file.Label, file.Name, file.AuthIndex),
		// AuthType 固定为 Auth File，与 API Key identity 隔离。
		AuthType: entities.UsageIdentityAuthTypeAuthFile,
		// AuthTypeName 与 usage_events 的 oauth 关联规则一致。
		AuthTypeName: "oauth",
		// Identity 只接收 CPA auth-index。
		Identity: file.AuthIndex,
		// Type 保留 CPA Auth File 原始类型。
		Type: file.Type,
		// Provider 保留 CPA 独立 provider 字段。
		Provider: file.Provider,
		// Prefix 保留 CPA 独立 prefix 字段。
		Prefix: file.Prefix,
		// FileName 保存 CPA name 的非空值，不参与展示名 fallback 反写。
		FileName: stringValue(file.Name),
		// FilePath 保存 CPA path 的非空值。
		FilePath: stringValue(file.Path),
		// Priority 原样保留 CPA nullable 语义。
		Priority: file.Priority,
		// Disabled 原样保留 CPA nullable 语义。
		Disabled: file.Disabled,
		// Note 原样保留 CPA nullable 语义。
		Note: file.Note,
	}
}

// extendCodexAuthFileUsageIdentity 只为 type=codex 写入 ChatGPT id_token 派生字段。
func extendCodexAuthFileUsageIdentity(file authfiles.AuthFile, identity *entities.UsageIdentity) {
	// AccountID 继续使用既有 exact-key 解析 helper。
	identity.AccountID = resolveCodexAccountID(file)
	// ActiveStart 继续规范化 id_token 的开始时间。
	identity.ActiveStart = resolveCodexActiveStart(file)
	// ActiveUntil 继续规范化 id_token 的结束时间。
	identity.ActiveUntil = resolveCodexActiveUntil(file)
	// PlanType 继续保留 id_token 的套餐类型。
	identity.PlanType = resolveCodexPlanType(file)
}

// extendXAIAuthFileUsageIdentity 只为 OAuth/Auth File xAI 身份写入 claims user id。
func extendXAIAuthFileUsageIdentity(file authfiles.AuthFile, identity *entities.UsageIdentity) {
	// 未命中候选时保持 nil，使成功重同步可以清掉旧值。
	identity.XAIUserID = resolveXAIUserID(file)
}
