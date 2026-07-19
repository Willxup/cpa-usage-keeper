package test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/cpa/dto/authfiles"
	"cpa-usage-keeper/internal/cpa/dto/cpaapikeys"
	"cpa-usage-keeper/internal/cpa/dto/providerconfig"
	"cpa-usage-keeper/internal/cpa/dto/response"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
	"gorm.io/gorm"
)

// TestSyncMetadataPreservesAuthFilesAndManagementAPIKeySemantics 锁定两类非 provider metadata 的既有映射结果。
func TestSyncMetadataPreservesAuthFilesAndManagementAPIKeySemantics(t *testing.T) {
	// db 使用真实 repository schema 验证最终持久化字段。
	db := openMetadataTestDatabase(t, "auth-and-management.db")
	// now 固定本轮所有 metadata 行的时间边界。
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	// activeStart 模拟 Codex id_token 的配额开始时间。
	activeStart := now.Add(-24 * time.Hour)
	// activeUntil 模拟 Codex id_token 的配额结束时间。
	activeUntil := now.Add(6 * 24 * time.Hour)
	// accountID 只允许写入 Codex Auth File。
	accountID := "acct-codex"
	// planType 只允许写入 Codex Auth File。
	planType := "team"
	// fetcher 默认 provider 全成功空列表，本测试只设置 Auth Files 与管理 API Keys。
	fetcher := newMetadataTestFetcher()
	// Auth Files 覆盖 email/label/name/auth-index fallback 和专属扩展字段。
	fetcher.setAuthFiles([]authfiles.AuthFile{
		// Claude 使用 email 作为展示名，并保留文件与同步 metadata。
		{AuthIndex: "auth-email", Name: "claude.json", Path: "/data/auths/claude.json", Email: "user@example.com", Label: "ignored-label", Type: "claude", Provider: "Claude", Prefix: "oauth-prefix", Priority: metadataIntPointer(6), Disabled: metadataBoolPointer(false), Note: metadataStringPointer("auth note")},
		// Gemini 使用 label fallback，并把独立 ProjectID 写入身份字段。
		{AuthIndex: "auth-label", Name: "gemini.json", Label: "Gemini Label", Type: "gemini", Provider: "Gemini", ProjectID: "project-gemini"},
		// Codex 使用 name fallback，并解析 id_token 专属字段。
		{AuthIndex: "auth-name", Name: "Codex Name", Type: "codex", Provider: "Codex", IDToken: &authfiles.AuthFileIDToken{AccountID: &accountID, ActiveStart: &activeStart, ActiveUntil: &activeUntil, PlanType: &planType}},
		// Vertex 缺少所有展示字段时回退到稳定 auth-index。
		{AuthIndex: "auth-index-fallback", Type: "vertex", Provider: "Vertex"},
	})
	// 管理 API Keys 保存原值，读取接口再返回脱敏 display key。
	fetcher.managementAPIKeysResult = &response.ManagementAPIKeysResult{StatusCode: 200, Payload: cpaapikeys.ManagementAPIKeysResponse{APIKeys: []string{"sk-alpha123456", "sk-beta654321"}}}
	// syncer 注入固定时钟和函数式 metadata fetcher。
	syncer := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{BaseURL: "https://cpa.example.com", MetadataFetcher: fetcher, Now: func() time.Time { return now }})
	// 执行一轮完整 metadata 同步。
	if err := syncer.SyncMetadata(context.Background()); err != nil {
		// 全部 endpoint 成功时不应返回 warning。
		t.Fatalf("SyncMetadata returned error: %v", err)
	}
	// Auth Files 每轮只读取一次。
	if fetcher.callCount("auth-files") != 1 {
		// 输出真实次数定位编排重复调用。
		t.Fatalf("auth-files calls = %d", fetcher.callCount("auth-files"))
	}
	// 管理 API Keys 每轮只读取一次。
	if fetcher.callCount("management-api-keys") != 1 {
		// 输出真实次数定位编排重复调用。
		t.Fatalf("management-api-keys calls = %d", fetcher.callCount("management-api-keys"))
	}
	// identities 按 auth_type + identity 读取最终数据库结果。
	identities := loadMetadataIdentityMap(t, db)
	// emailRow 验证 Auth File 的通用字段映射。
	emailRow := identities[metadataIdentityKey(entities.UsageIdentityAuthTypeAuthFile, "auth-email")]
	// email 必须优先于 label/name，且 OAuth 关联字段保持原语义。
	if emailRow.Name != "user@example.com" || emailRow.AuthTypeName != "oauth" || emailRow.Type != "claude" || emailRow.Provider != "Claude" || emailRow.Prefix != "oauth-prefix" || emailRow.IsDeleted {
		// 输出完整行定位字段来源错误。
		t.Fatalf("email auth identity = %+v", emailRow)
	}
	// 文件名与路径只作为 Auth File metadata 保存。
	if emailRow.FileName == nil || *emailRow.FileName != "claude.json" || emailRow.FilePath == nil || *emailRow.FilePath != "/data/auths/claude.json" {
		// 输出完整行定位 nullable 字段错误。
		t.Fatalf("email auth file metadata = %+v", emailRow)
	}
	// priority/disabled/note 必须保留 CPA 可选值。
	if emailRow.Priority == nil || *emailRow.Priority != 6 || emailRow.Disabled == nil || *emailRow.Disabled || emailRow.Note == nil || *emailRow.Note != "auth note" {
		// 输出完整行定位可选字段错误。
		t.Fatalf("email auth optional metadata = %+v", emailRow)
	}
	// labelRow 验证 label fallback 与 Gemini project_id。
	labelRow := identities[metadataIdentityKey(entities.UsageIdentityAuthTypeAuthFile, "auth-label")]
	// label 在 email 缺失时优先于 name。
	if labelRow.Name != "Gemini Label" || labelRow.ProjectID == nil || *labelRow.ProjectID != "project-gemini" {
		// 输出完整行定位 fallback 或 project_id 错误。
		t.Fatalf("label auth identity = %+v", labelRow)
	}
	// codexRow 验证 Codex id_token 扩展不影响通用字段。
	codexRow := identities[metadataIdentityKey(entities.UsageIdentityAuthTypeAuthFile, "auth-name")]
	// Codex name fallback 与 account/plan/window 必须完整保存。
	if codexRow.Name != "Codex Name" || codexRow.AccountID == nil || *codexRow.AccountID != accountID || codexRow.PlanType == nil || *codexRow.PlanType != planType || codexRow.ActiveStart == nil || !codexRow.ActiveStart.Equal(activeStart) || codexRow.ActiveUntil == nil || !codexRow.ActiveUntil.Equal(activeUntil) {
		// 输出完整行定位 Codex 扩展错误。
		t.Fatalf("codex auth identity = %+v", codexRow)
	}
	// fallbackRow 验证最后回退到 auth-index。
	fallbackRow := identities[metadataIdentityKey(entities.UsageIdentityAuthTypeAuthFile, "auth-index-fallback")]
	// 无展示字段时 name 必须稳定等于 auth-index。
	if fallbackRow.Name != "auth-index-fallback" {
		// 输出完整行定位 fallback 顺序错误。
		t.Fatalf("fallback auth identity = %+v", fallbackRow)
	}
	// 所有新建身份必须采用本轮 now，不能使用 GORM 系统时钟或零时间。
	if emailRow.CreatedAt.IsZero() || !emailRow.CreatedAt.Equal(now) || emailRow.UpdatedAt.IsZero() || !emailRow.UpdatedAt.Equal(now) {
		// 输出两个时间帮助定位 repository 时间合同。
		t.Fatalf("auth identity times = created:%s updated:%s", emailRow.CreatedAt, emailRow.UpdatedAt)
	}
	// apiKeys 通过公开 repository 读取 active 管理 key。
	apiKeys, err := repository.ListActiveCPAAPIKeys(db)
	// 管理 key 查询必须成功。
	if err != nil {
		// 输出查询错误。
		t.Fatalf("ListActiveCPAAPIKeys returned error: %v", err)
	}
	// 两条 key 必须保存，展示值继续沿用旧脱敏格式。
	if len(apiKeys) != 2 || apiKeys[0].DisplayKey != "sk-*********123456" || apiKeys[0].APIKey != "sk-alpha123456" {
		// 输出真实行定位排序或脱敏差异。
		t.Fatalf("management API keys = %+v", apiKeys)
	}
}

// TestSyncMetadataFetchFailuresPreserveAuthFilesAndManagementKeys 验证失败来源不执行 stale 或删除本地数据。
func TestSyncMetadataFetchFailuresPreserveAuthFilesAndManagementKeys(t *testing.T) {
	// db 保存首轮成功数据与次轮失败后的状态。
	db := openMetadataTestDatabase(t, "preserve-on-fetch-failure.db")
	// firstNow 是首轮成功同步时间。
	firstNow := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	// secondNow 是失败轮时间，数据库行不应被它刷新。
	secondNow := firstNow.Add(time.Hour)
	// fetcher 首轮提供一条 Auth File 和一条管理 key。
	fetcher := newMetadataTestFetcher()
	// Auth File 成功输入建立待保留行。
	fetcher.setAuthFiles([]authfiles.AuthFile{{AuthIndex: "preserved-auth", Email: "preserved@example.com", Type: "claude", Provider: "Claude"}})
	// 管理 key 成功输入建立待保留行。
	fetcher.managementAPIKeysResult = &response.ManagementAPIKeysResult{StatusCode: 200, Payload: cpaapikeys.ManagementAPIKeysResponse{APIKeys: []string{"sk-preserved123456"}}}
	// currentNow 允许同一 syncer 在两轮间切换固定时间。
	currentNow := firstNow
	// syncer 使用闭包读取当前轮时间。
	syncer := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{BaseURL: "https://cpa.example.com", MetadataFetcher: fetcher, Now: func() time.Time { return currentNow }})
	// 首轮必须成功建立本地数据。
	if err := syncer.SyncMetadata(context.Background()); err != nil {
		// 首轮失败无法验证 preserve 合同。
		t.Fatalf("initial SyncMetadata returned error: %v", err)
	}
	// 次轮切换到独立时间。
	currentNow = secondNow
	// Auth Files 返回 fetch error 且没有可用 result。
	fetcher.authFilesResult = nil
	// 错误文本用于验证最终错误顺序。
	fetcher.authFilesErr = errors.New("auth unavailable")
	// 管理 API Keys 返回独立 fetch error。
	fetcher.managementAPIKeysResult = nil
	// 错误文本用于验证最终错误顺序。
	fetcher.managementAPIKeysErr = errors.New("management unavailable")
	// 执行失败轮并保留合并错误。
	err := syncer.SyncMetadata(context.Background())
	// 两类错误必须按 Auth Files、管理 API Keys 顺序稳定返回。
	if err == nil || err.Error() != "fetch auth files: auth unavailable; fetch management api keys: management unavailable" {
		// 输出真实错误定位分类或顺序漂移。
		t.Fatalf("failure error = %v", err)
	}
	// identities 读取失败轮后的 Auth File。
	identities := loadMetadataIdentityMap(t, db)
	// preservedAuth 必须保持 active 与首轮时间。
	preservedAuth := identities[metadataIdentityKey(entities.UsageIdentityAuthTypeAuthFile, "preserved-auth")]
	// fetch failure 不能把本地身份 stale 或刷新 updated_at。
	if preservedAuth.IsDeleted || preservedAuth.DeletedAt != nil || !preservedAuth.UpdatedAt.Equal(firstNow) {
		// 输出完整行定位错误写入。
		t.Fatalf("preserved auth identity = %+v", preservedAuth)
	}
	// apiKeys 读取失败轮后的 active key。
	apiKeys, listErr := repository.ListActiveCPAAPIKeys(db)
	// 查询必须成功。
	if listErr != nil {
		// 输出查询错误。
		t.Fatalf("ListActiveCPAAPIKeys returned error: %v", listErr)
	}
	// fetch failure 不能删除首轮 key。
	if len(apiKeys) != 1 || apiKeys[0].APIKey != "sk-preserved123456" {
		// 输出真实行定位错误 stale。
		t.Fatalf("preserved management API keys = %+v", apiKeys)
	}
}

// TestSyncMetadataProviderWarningStillRunsHistoricalStatsCatchUp 验证 provider warning 不阻止成功身份补算历史 usage。
func TestSyncMetadataProviderWarningStillRunsHistoricalStatsCatchUp(t *testing.T) {
	// db 保存先于 identity 到达的历史 usage event。
	db := openMetadataTestDatabase(t, "provider-warning-catch-up.db")
	// eventTime 早于 metadata 同步，证明新建身份需要历史补算。
	eventTime := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	// now 是 identity 新建与 stats_updated_at 的统一时间。
	now := eventTime.Add(time.Hour)
	// 历史事件使用 apikey + auth-index 精确关联 Codex provider identity。
	_, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{EventKey: "provider-history", AuthType: "apikey", AuthIndex: "codex-history", Model: "gpt-5", Timestamp: eventTime, InputTokens: 11, OutputTokens: 13, TotalTokens: 24}})
	// seed 必须成功才能验证 catch-up。
	if err != nil {
		// 输出事件写入错误。
		t.Fatalf("seed provider usage event: %v", err)
	}
	// fetcher 让 Codex 成功、Gemini 失败，其它来源成功空列表。
	fetcher := newMetadataTestFetcher()
	// Codex 成功返回稳定 API Key/auth-index 字段。
	fetcher.standardResults["codex"] = &response.ProviderKeyConfigResult{StatusCode: 200, Payload: []providerconfig.ProviderKeyConfig{{APIKey: "codex-secret", Name: "Codex History", AuthIndex: "codex-history"}}}
	// Gemini warning 不应阻止 Codex identity 落库和统计补算。
	fetcher.standardResults["gemini"] = nil
	// 注入明确 Gemini fetch error。
	fetcher.standardErrors["gemini"] = errors.New("gemini unavailable")
	// syncer 使用固定本轮时间。
	syncer := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{BaseURL: "https://cpa.example.com", MetadataFetcher: fetcher, Now: func() time.Time { return now }})
	// 执行部分成功同步。
	err = syncer.SyncMetadata(context.Background())
	// warning 必须返回给调用方。
	if err == nil || !strings.Contains(err.Error(), "fetch gemini api keys: gemini unavailable") {
		// 输出真实错误定位 warning 丢失。
		t.Fatalf("provider warning = %v", err)
	}
	// identities 读取成功 Codex 行。
	identities := loadMetadataIdentityMap(t, db)
	// codexRow 使用 AI Provider auth type 隔离 OAuth。
	codexRow := identities[metadataIdentityKey(entities.UsageIdentityAuthTypeAIProvider, "codex-history")]
	// 成功来源必须落库并从历史事件补算全部统计。
	if codexRow.Identity != "codex-history" || codexRow.LookupKey != "codex-secret" || codexRow.TotalRequests != 1 || codexRow.InputTokens != 11 || codexRow.OutputTokens != 13 || codexRow.TotalTokens != 24 || codexRow.LastAggregatedUsageEventID == 0 {
		// 输出完整行定位 persistence 或 catch-up 错误。
		t.Fatalf("provider catch-up identity = %+v", codexRow)
	}
	// 首尾使用时间必须来自历史事件。
	if codexRow.FirstUsedAt == nil || !codexRow.FirstUsedAt.Equal(eventTime) || codexRow.LastUsedAt == nil || !codexRow.LastUsedAt.Equal(eventTime) || codexRow.StatsUpdatedAt == nil || !codexRow.StatsUpdatedAt.Equal(now) {
		// 输出完整行定位历史时间补算错误。
		t.Fatalf("provider catch-up times = %+v", codexRow)
	}
}

// TestSyncMetadataProviderPersistenceErrorSuppressesFetchWarning 验证 provider 事务失败时回滚并沿用旧 warning 抑制规则。
func TestSyncMetadataProviderPersistenceErrorSuppressesFetchWarning(t *testing.T) {
	// db 保留 Auth Files 与管理 key 的独立事务，同时只阻断 provider identity create。
	db := openMetadataTestDatabase(t, "provider-persistence-error.db")
	// now 固定本轮 metadata 时间。
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	// fetcher 让 Codex 成功并让 Gemini 同时产生 fetch warning。
	fetcher := newMetadataTestFetcher()
	// 管理 key 成功输入证明前面的独立事务不会因 provider 回滚而丢失。
	fetcher.managementAPIKeysResult = &response.ManagementAPIKeysResult{StatusCode: 200, Payload: cpaapikeys.ManagementAPIKeysResponse{APIKeys: []string{"sk-independent123456"}}}
	// Codex 成功行会触发 provider CreateInBatches。
	fetcher.standardResults["codex"] = &response.ProviderKeyConfigResult{StatusCode: 200, Payload: []providerconfig.ProviderKeyConfig{{APIKey: "codex-secret", AuthIndex: "codex-persist-fail", Name: "Codex"}}}
	// Gemini 没有可用 result。
	fetcher.standardResults["gemini"] = nil
	// Gemini warning 应被后续 provider persistence error 抑制。
	fetcher.standardErrors["gemini"] = errors.New("gemini unavailable")
	// callback 只拦截 usage_identities create，不影响管理 key 独立表。
	if err := db.Callback().Create().Before("gorm:create").Register("test:block_provider_identity_create", func(tx *gorm.DB) {
		// 仅目标表触发可识别 persistence error。
		if tx.Statement != nil && tx.Statement.Table == "usage_identities" {
			// AddError 让当前 provider 事务回滚。
			tx.AddError(errors.New("provider persistence blocked"))
		}
	}); err != nil {
		// callback 注册失败属于测试环境错误。
		t.Fatalf("register provider persistence callback: %v", err)
	}
	// syncer 注入固定时间与部分失败 fetcher。
	syncer := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{BaseURL: "https://cpa.example.com", MetadataFetcher: fetcher, Now: func() time.Time { return now }})
	// 执行一轮 provider persistence 失败同步。
	err := syncer.SyncMetadata(context.Background())
	// 返回必须是 provider persistence error。
	if err == nil || !strings.Contains(err.Error(), "sync provider usage identities: create usage identities: provider persistence blocked") {
		// 输出真实错误定位事务包装差异。
		t.Fatalf("provider persistence error = %v", err)
	}
	// 同轮 Gemini fetch warning 必须沿用旧逻辑被抑制。
	if strings.Contains(err.Error(), "gemini unavailable") {
		// 同时返回 warning 会改变旧错误优先级。
		t.Fatalf("provider fetch warning was not suppressed: %v", err)
	}
	// provider 单事务回滚后不能留下部分 Codex identity。
	identities := loadMetadataIdentityMap(t, db)
	// 目标 identity 必须不存在。
	if _, ok := identities[metadataIdentityKey(entities.UsageIdentityAuthTypeAIProvider, "codex-persist-fail")]; ok {
		// 输出行集合定位事务回滚失败。
		t.Fatalf("provider identity survived rollback: %+v", identities)
	}
	// 管理 key 独立事务必须保留已提交结果。
	apiKeys, listErr := repository.ListActiveCPAAPIKeys(db)
	// 查询管理 key 必须成功。
	if listErr != nil {
		// 输出查询错误。
		t.Fatalf("ListActiveCPAAPIKeys returned error: %v", listErr)
	}
	// provider 回滚不能反向回滚管理 key。
	if len(apiKeys) != 1 || apiKeys[0].APIKey != "sk-independent123456" {
		// 输出真实 key 行定位事务边界错误。
		t.Fatalf("independent management keys = %+v", apiKeys)
	}
}

// TestSyncMetadataManagementPersistenceErrorKeepsProviderWriteAndStopsCatchUp 验证独立事务部分成功与统计门槛。
func TestSyncMetadataManagementPersistenceErrorKeepsProviderWriteAndStopsCatchUp(t *testing.T) {
	// db 保存历史 provider event 和本轮成功 provider identity。
	db := openMetadataTestDatabase(t, "management-persistence-error.db")
	// eventTime 早于 metadata，用于判断 catch-up 是否被正确阻止。
	eventTime := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	// now 固定本轮 metadata 时间。
	now := eventTime.Add(time.Hour)
	// 历史 event 若执行 aggregate 会把 identity TotalRequests 更新为一。
	_, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{EventKey: "blocked-catch-up", AuthType: "apikey", AuthIndex: "provider-survives", Model: "gpt-5", Timestamp: eventTime, TotalTokens: 10}})
	// seed 必须成功。
	if err != nil {
		// 输出事件写入错误。
		t.Fatalf("seed blocked catch-up event: %v", err)
	}
	// 删除管理 key 表，只让该类别的 persistence 失败。
	if err := db.Migrator().DropTable(&entities.CPAAPIKey{}); err != nil {
		// schema 准备失败不属于待测逻辑。
		t.Fatalf("drop cpa_api_keys table: %v", err)
	}
	// fetcher 为管理 key 和 Codex provider 都返回成功数据。
	fetcher := newMetadataTestFetcher()
	// 非空管理 key 确保 repository 真正访问已删除表。
	fetcher.managementAPIKeysResult = &response.ManagementAPIKeysResult{StatusCode: 200, Payload: cpaapikeys.ManagementAPIKeysResponse{APIKeys: []string{"sk-fails123456"}}}
	// provider 成功行应在自己的后续事务中提交。
	fetcher.standardResults["codex"] = &response.ProviderKeyConfigResult{StatusCode: 200, Payload: []providerconfig.ProviderKeyConfig{{APIKey: "provider-secret", AuthIndex: "provider-survives", Name: "Provider Survives"}}}
	// syncer 注入固定时间。
	syncer := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{BaseURL: "https://cpa.example.com", MetadataFetcher: fetcher, Now: func() time.Time { return now }})
	// 执行管理 persistence 失败但 provider 成功的一轮。
	err = syncer.SyncMetadata(context.Background())
	// 错误必须归类为管理 API Keys persistence failure。
	if err == nil || !strings.Contains(err.Error(), "sync management api keys") {
		// 输出真实错误定位分类漂移。
		t.Fatalf("management persistence error = %v", err)
	}
	// provider 后续独立事务仍必须提交。
	identities := loadMetadataIdentityMap(t, db)
	// providerRow 读取成功 Codex identity。
	providerRow := identities[metadataIdentityKey(entities.UsageIdentityAuthTypeAIProvider, "provider-survives")]
	// provider metadata 已写入，但统计补算因任一 upsert error 被阻止。
	if providerRow.Identity != "provider-survives" || providerRow.LookupKey != "provider-secret" || providerRow.IsDeleted || providerRow.TotalRequests != 0 || providerRow.LastAggregatedUsageEventID != 0 || providerRow.StatsUpdatedAt != nil {
		// 输出完整行定位事务边界或 catch-up 门槛错误。
		t.Fatalf("provider after management persistence error = %+v", providerRow)
	}
}

// TestSyncMetadataInteractionsRestoreCatchesUpOnlyNewHistoricalEvents 验证新来源恢复时保留 alias/统计/cursor 并只补算新增事件。
func TestSyncMetadataInteractionsRestoreCatchesUpOnlyNewHistoricalEvents(t *testing.T) {
	// db 保存 Interactions identity 的新建、stale、恢复与重复同步状态。
	db := openMetadataTestDatabase(t, "interactions-restore-catch-up.db")
	// firstEventTime 是 identity 创建前已经存在的历史事件。
	firstEventTime := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	// firstSyncTime 是首次创建和历史补算时间。
	firstSyncTime := firstEventTime.Add(time.Hour)
	// staleTime 是成功空列表删除 identity 的时间。
	staleTime := firstSyncTime.Add(time.Hour)
	// secondEventTime 是 stale 后新增、待恢复补算的事件。
	secondEventTime := staleTime.Add(time.Hour)
	// restoreTime 是 identity 恢复并追平第二条事件的时间。
	restoreTime := secondEventTime.Add(time.Hour)
	// 首条历史事件使用 apikey + auth-index 精确关联 Interactions。
	_, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{EventKey: "interactions-history-1", AuthType: "apikey", AuthIndex: "interactions-history", Model: "gemini-2.5", Timestamp: firstEventTime, InputTokens: 3, OutputTokens: 5, TotalTokens: 8}})
	// 首条事件必须成功写入。
	if err != nil {
		// 输出事件写入错误。
		t.Fatalf("seed first Interactions event: %v", err)
	}
	// fetcher 默认其它来源成功空列表，Interactions 返回一条稳定 credential。
	fetcher := newMetadataTestFetcher()
	// Interactions 首轮成功输入建立新 identity。
	fetcher.standardResults["gemini-interactions"] = &response.ProviderKeyConfigResult{StatusCode: 200, Payload: []providerconfig.ProviderKeyConfig{{APIKey: "interactions-secret", AuthIndex: "interactions-history", Name: "Interactions History"}}}
	// currentNow 支持同一 syncer 依次执行四轮。
	currentNow := firstSyncTime
	// syncer 使用固定当前轮时间。
	syncer := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{BaseURL: "https://cpa.example.com", MetadataFetcher: fetcher, Now: func() time.Time { return currentNow }})
	// 首轮新建并补算第一条历史事件。
	if err := syncer.SyncMetadata(context.Background()); err != nil {
		// 首轮全部 endpoint 成功，不应返回错误。
		t.Fatalf("initial Interactions SyncMetadata returned error: %v", err)
	}
	// initialRows 读取首次补算后的 identity。
	initialRows := loadMetadataIdentityMap(t, db)
	// initial 保存后续需要验证的 ID、统计和 cursor。
	initial := initialRows[metadataIdentityKey(entities.UsageIdentityAuthTypeAIProvider, "interactions-history")]
	// 首轮必须创建 Interactions 类型并补算一条事件。
	if initial.ID == 0 || initial.Type != "gemini-interactions" || initial.TotalRequests != 1 || initial.InputTokens != 3 || initial.OutputTokens != 5 || initial.TotalTokens != 8 || initial.LastAggregatedUsageEventID == 0 {
		// 输出完整行定位新来源 catch-up 错误。
		t.Fatalf("initial Interactions identity = %+v", initial)
	}
	// 保存首轮 cursor，恢复后必须只从它之后继续。
	firstCursor := initial.LastAggregatedUsageEventID
	// alias 是 Keeper-only 字段，恢复不能清空。
	if err := repository.UpdateUsageIdentityAlias(context.Background(), db, initial.ID, "Local Interactions Alias"); err != nil {
		// alias 写入失败无法验证恢复保留语义。
		t.Fatalf("set Interactions alias: %v", err)
	}
	// 第二轮 Interactions 成功返回空列表，进入 scoped stale。
	fetcher.standardResults["gemini-interactions"] = &response.ProviderKeyConfigResult{StatusCode: 200, Payload: []providerconfig.ProviderKeyConfig{}}
	// 切换到 stale 时间。
	currentNow = staleTime
	// 成功空列表轮必须完成 stale。
	if err := syncer.SyncMetadata(context.Background()); err != nil {
		// 输出真实错误定位 stale 轮异常。
		t.Fatalf("stale Interactions SyncMetadata returned error: %v", err)
	}
	// 第二条事件在 identity stale 后到达，恢复时应按 cursor 补算。
	_, _, err = repository.InsertUsageEvents(db, []entities.UsageEvent{{EventKey: "interactions-history-2", AuthType: "apikey", AuthIndex: "interactions-history", Model: "gemini-2.5", Timestamp: secondEventTime, InputTokens: 7, OutputTokens: 11, TotalTokens: 18}})
	// 第二条事件必须成功写入。
	if err != nil {
		// 输出事件写入错误。
		t.Fatalf("seed second Interactions event: %v", err)
	}
	// 第三轮重新返回同一 auth-index，使 deleted identity 恢复。
	fetcher.standardResults["gemini-interactions"] = &response.ProviderKeyConfigResult{StatusCode: 200, Payload: []providerconfig.ProviderKeyConfig{{APIKey: "interactions-secret-refreshed", AuthIndex: "interactions-history", Name: "Interactions Restored"}}}
	// 切换到恢复时间。
	currentNow = restoreTime
	// 恢复轮必须成功并执行历史统计 catch-up。
	if err := syncer.SyncMetadata(context.Background()); err != nil {
		// 输出真实错误定位 restore/catch-up 异常。
		t.Fatalf("restore Interactions SyncMetadata returned error: %v", err)
	}
	// restoredRows 读取恢复后的最终 identity。
	restoredRows := loadMetadataIdentityMap(t, db)
	// restored 必须命中同一数据库行而非创建重复身份。
	restored := restoredRows[metadataIdentityKey(entities.UsageIdentityAuthTypeAIProvider, "interactions-history")]
	// 恢复保持 ID、created_at、alias，并刷新 metadata/LookupKey 与 active 状态。
	if restored.ID != initial.ID || !restored.CreatedAt.Equal(initial.CreatedAt) || restored.Alias == nil || *restored.Alias != "Local Interactions Alias" || restored.IsDeleted || restored.DeletedAt != nil || restored.Name != "Interactions Restored" || restored.LookupKey != "interactions-secret-refreshed" {
		// 输出完整行定位恢复字段或 Keeper-only 数据丢失。
		t.Fatalf("restored Interactions metadata = %+v", restored)
	}
	// 统计只增加第二条事件，首轮累计不能清零或重复。
	if restored.TotalRequests != 2 || restored.InputTokens != 10 || restored.OutputTokens != 16 || restored.TotalTokens != 26 || restored.LastAggregatedUsageEventID <= firstCursor {
		// 输出完整行定位 cursor 或增量统计错误。
		t.Fatalf("restored Interactions stats = %+v", restored)
	}
	// 第四轮输入完全相同，只验证重复同步不会重复累计。
	currentNow = restoreTime.Add(time.Hour)
	// 重复 metadata refresh 必须成功。
	if err := syncer.SyncMetadata(context.Background()); err != nil {
		// 输出真实错误定位幂等同步异常。
		t.Fatalf("repeat Interactions SyncMetadata returned error: %v", err)
	}
	// repeatedRows 读取重复同步后的统计。
	repeatedRows := loadMetadataIdentityMap(t, db)
	// repeated 必须保持两条事件累计与同一 cursor。
	repeated := repeatedRows[metadataIdentityKey(entities.UsageIdentityAuthTypeAIProvider, "interactions-history")]
	// 只允许 updated_at 刷新，统计和 cursor 不能再变化。
	if repeated.TotalRequests != 2 || repeated.InputTokens != 10 || repeated.OutputTokens != 16 || repeated.TotalTokens != 26 || repeated.LastAggregatedUsageEventID != restored.LastAggregatedUsageEventID {
		// 输出完整行定位重复补算。
		t.Fatalf("repeated Interactions stats = %+v", repeated)
	}
}
