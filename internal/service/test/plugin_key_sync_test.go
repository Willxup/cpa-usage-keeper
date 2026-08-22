package test

import (
	"context"
	"errors"
	"testing"
	"time"

	"cpa-usage-keeper/internal/cpa/dto/keypolicy"
	"cpa-usage-keeper/internal/cpa/dto/response"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
)

// TestSyncMetadataSyncsPluginKeysWhenEnabled 验证开关启用时插件 key 随 CPA key 同步成功后写入本地。
func TestSyncMetadataSyncsPluginKeysWhenEnabled(t *testing.T) {
	// db 使用真实 repository schema 验证最终持久化字段。
	db := openMetadataTestDatabase(t, "plugin-key-sync.db")
	// now 固定本轮所有 metadata 行的时间边界。
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	// fetcher 默认管理 API Keys 成功空列表，本测试补充一条插件 key。
	fetcher := newMetadataTestFetcher()
	fetcher.keyPolicyPluginKeysResult = &response.KeyPolicyPluginKeysResult{
		StatusCode: 200,
		Payload:    keypolicy.PluginKeysResponse{Keys: []keypolicy.PluginKey{{ID: "kath", Name: "凯瑟琳", Enabled: true, KeyPreview: "sk-8bc0...64426"}}},
	}
	// syncer 注入固定时钟、函数式 fetcher 和启用的插件同步开关。
	syncer := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{
		BaseURL:              "https://cpa.example.com",
		MetadataFetcher:      fetcher,
		Now:                  func() time.Time { return now },
		KeyPolicySyncEnabled: true,
	})

	// 执行一轮完整 metadata 同步。
	if err := syncer.SyncMetadata(context.Background()); err != nil {
		// 全部 endpoint 成功时不应返回 warning。
		t.Fatalf("SyncMetadata returned error: %v", err)
	}
	// 插件 keys 每轮只在 CPA key 同步成功后读取一次。
	if fetcher.callCount("key-policy-plugin-keys") != 1 {
		t.Fatalf("key-policy-plugin-keys calls = %d", fetcher.callCount("key-policy-plugin-keys"))
	}
	// 插件行必须保存 id、插件别名和来源标记。
	var row entities.CPAAPIKey
	if err := db.Where("api_key = ?", "kath").First(&row).Error; err != nil {
		t.Fatalf("expected plugin key row: %v", err)
	}
	if row.Source != entities.CPAAPIKeySourcePlugin || row.DisplayKey != "sk-8bc0...64426" || row.KeyAlias != "凯瑟琳" || row.IsDeleted {
		t.Fatalf("unexpected plugin key row: %+v", row)
	}
}

// TestSyncMetadataPluginFetchFailurePreservesPluginRows 验证插件拉取失败只记日志，本地 plugin rows 保持现状。
func TestSyncMetadataPluginFetchFailurePreservesPluginRows(t *testing.T) {
	// db 保存首轮成功数据与次轮失败后的状态。
	db := openMetadataTestDatabase(t, "plugin-key-sync-failure.db")
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	fetcher := newMetadataTestFetcher()
	fetcher.keyPolicyPluginKeysResult = &response.KeyPolicyPluginKeysResult{
		StatusCode: 200,
		Payload:    keypolicy.PluginKeysResponse{Keys: []keypolicy.PluginKey{{ID: "kath", Name: "凯瑟琳", Enabled: true, KeyPreview: "sk-8bc0...64426"}}},
	}
	syncer := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{
		BaseURL:              "https://cpa.example.com",
		MetadataFetcher:      fetcher,
		Now:                  func() time.Time { return now },
		KeyPolicySyncEnabled: true,
	})
	// 首轮成功同步写入一条 plugin row。
	if err := syncer.SyncMetadata(context.Background()); err != nil {
		t.Fatalf("first SyncMetadata returned error: %v", err)
	}

	// 第二轮插件拉取失败；fetch failure 没有完整新清单，必须保留本地 plugin rows。
	fetcher.keyPolicyPluginKeysResult = nil
	fetcher.keyPolicyPluginKeysErr = errors.New("plugin endpoint unavailable")
	if err := syncer.SyncMetadata(context.Background()); err != nil {
		// 插件失败只记日志，不允许以 warning 形式出现在主流程返回值里。
		t.Fatalf("SyncMetadata with plugin fetch failure returned error: %v", err)
	}
	row, err := repository.FindActiveCPAAPIKeyByID(db, 1)
	if err != nil {
		t.Fatalf("expected plugin row to survive fetch failure: %v", err)
	}
	if row.APIKey != "kath" || row.IsDeleted {
		t.Fatalf("expected plugin row to stay active after fetch failure, got %+v", row)
	}
}

// TestSyncMetadataSkipsPluginKeysWhenDisabled 锁定开关关闭时的零行为变化：不发起插件请求，也不写 plugin rows。
func TestSyncMetadataSkipsPluginKeysWhenDisabled(t *testing.T) {
	// db 验证关闭开关后数据库不产生任何 plugin 行。
	db := openMetadataTestDatabase(t, "plugin-key-disabled.db")
	fetcher := newMetadataTestFetcher()
	fetcher.keyPolicyPluginKeysResult = &response.KeyPolicyPluginKeysResult{
		StatusCode: 200,
		Payload:    keypolicy.PluginKeysResponse{Keys: []keypolicy.PluginKey{{ID: "kath", Name: "凯瑟琳", Enabled: true, KeyPreview: "sk-8bc0...64426"}}},
	}
	// 默认构造不带 KeyPolicySyncEnabled，保持既有行为。
	syncer := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{
		BaseURL:         "https://cpa.example.com",
		MetadataFetcher: fetcher,
		Now:             func() time.Time { return time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC) },
	})

	if err := syncer.SyncMetadata(context.Background()); err != nil {
		t.Fatalf("SyncMetadata returned error: %v", err)
	}
	// 开关关闭时绝不能访问插件 endpoint。
	if fetcher.callCount("key-policy-plugin-keys") != 0 {
		t.Fatalf("expected no plugin fetch when disabled, got %d calls", fetcher.callCount("key-policy-plugin-keys"))
	}
	var count int64
	if err := db.Model(&entities.CPAAPIKey{}).Where("source = ?", entities.CPAAPIKeySourcePlugin).Count(&count).Error; err != nil {
		t.Fatalf("count plugin rows: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no plugin rows when disabled, got %d", count)
	}
}

// TestSyncMetadataSkipsPluginKeysWhenCPASyncFails 验证 CPA key 同步失败时不得触碰本地 plugin rows。
func TestSyncMetadataSkipsPluginKeysWhenCPASyncFails(t *testing.T) {
	// db 先用直写 repository 准备一条 plugin row，再验证 CPA 失败后它保持 active。
	db := openMetadataTestDatabase(t, "plugin-key-cpa-failure.db")
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	if err := repository.SyncPluginAPIKeys(db, []repository.PluginAPIKey{{ID: "kath", Name: "凯瑟琳"}}, now); err != nil {
		t.Fatalf("seed plugin key: %v", err)
	}
	// fetcher 注入管理 API Keys fetch failure；CPA 侧没有完整新清单时插件同步也不允许执行。
	fetcher := newMetadataTestFetcher()
	fetcher.managementAPIKeysErr = errors.New("management endpoint unavailable")
	fetcher.keyPolicyPluginKeysResult = &response.KeyPolicyPluginKeysResult{StatusCode: 200, Payload: keypolicy.PluginKeysResponse{}}
	syncer := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{
		BaseURL:              "https://cpa.example.com",
		MetadataFetcher:      fetcher,
		Now:                  func() time.Time { return now },
		KeyPolicySyncEnabled: true,
	})

	// 管理 key 失败仍作为 warning 返回，但插件 rows 不能随之被替换。
	if err := syncer.SyncMetadata(context.Background()); err == nil {
		t.Fatal("expected SyncMetadata to report management api keys failure")
	}
	if fetcher.callCount("key-policy-plugin-keys") != 0 {
		t.Fatalf("expected no plugin fetch when CPA sync failed, got %d calls", fetcher.callCount("key-policy-plugin-keys"))
	}
	row, err := repository.FindActiveCPAAPIKeyByID(db, 1)
	if err != nil || row.APIKey != "kath" || row.IsDeleted {
		t.Fatalf("expected plugin row to stay active after CPA sync failure, got %+v err=%v", row, err)
	}
}
