package test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/helper"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
	"gorm.io/gorm"
)

// TestProviderMetadataSecretStaysOutOfUsageIdentityResponses 验证 CPA api-key 只留在 LookupKey，不进入列表 API。
func TestProviderMetadataSecretStaysOutOfUsageIdentityResponses(t *testing.T) {
	// db 保存字段值彼此完全不同的 AI Provider identity。
	db := openProviderMetadataSecretDatabase(t)
	// secret 只允许写入数据库 LookupKey。
	secret := "unique-provider-secret-7f18c2"
	// authIndex 是业务 identity，接口仍按敏感标识脱敏展示。
	authIndex := "provider-auth-index-91a6"
	// prefix 是独立可展示 metadata，不能由 secret 派生。
	prefix := "provider-prefix-visible"
	// baseURL 是独立内部 metadata，当前 API DTO 不发布该字段。
	baseURL := "https://provider-base-url.example/v1"
	// now 固定 seed identity 的存储时间。
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	// 通过公开 provider replace 写入与生产映射相同的目标实体。
	if err := repository.ReplaceUsageIdentitiesForProviderTypes(context.Background(), db, []entities.UsageIdentity{{Name: "Visible Provider", AuthTypeName: "apikey", Identity: authIndex, Type: "xai", Provider: "Visible Provider", LookupKey: secret, Prefix: prefix, BaseURL: baseURL}}, []string{"xai"}, now); err != nil {
		// seed 失败不属于 API 安全合同。
		t.Fatalf("seed provider metadata identity: %v", err)
	}
	// stored 直接读取数据库，证明测试确实把 secret 只写入 LookupKey。
	var stored entities.UsageIdentity
	// 使用 auth_type + identity 精确定位 provider 行。
	if err := db.Where("auth_type = ? AND identity = ?", entities.UsageIdentityAuthTypeAIProvider, authIndex).First(&stored).Error; err != nil {
		// 查询失败表示 seed 未建立预期行。
		t.Fatalf("load provider metadata identity: %v", err)
	}
	// 数据库内部 LookupKey 必须保留完整 secret，且其它字段保持独立值。
	if stored.LookupKey != secret || stored.Identity != authIndex || stored.Prefix != prefix || stored.BaseURL != baseURL || stored.Name != "Visible Provider" {
		// 输出完整行定位字段混用。
		t.Fatalf("stored provider metadata fields = %+v", stored)
	}
	// router 使用真实 UsageIdentity service 读取数据库。
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{UsageIdentity: service.NewUsageIdentityService(db)})
	// 同时检查旧列表与分页列表两个公开响应入口。
	for _, path := range []string{"/api/v1/usage/identities", "/api/v1/usage/identities/page?auth_type=2&page=1&page_size=10"} {
		// resp 记录当前 API 响应。
		resp := httptest.NewRecorder()
		// req 使用真实 GET 路由。
		req := httptest.NewRequest(http.MethodGet, path, nil)
		// 让 router 完成 service 查询和 DTO 映射。
		router.ServeHTTP(resp, req)
		// body 保留原始 JSON 供 secret 与字段名检查。
		body := resp.Body.String()
		// 两个列表入口都必须成功。
		if resp.Code != http.StatusOK {
			// 输出 path、状态和 body 定位路由差异。
			t.Fatalf("%s status = %d body=%s", path, resp.Code, body)
		}
		// 完整 secret 不能出现在任何响应值或错误文本中。
		if strings.Contains(body, secret) {
			// 输出 body 证明泄漏位置。
			t.Fatalf("%s leaked provider secret: %s", path, body)
		}
		// DTO 结构不能发布 lookup_key 字段名。
		if strings.Contains(body, "lookup_key") {
			// 输出 body 定位意外字段扩展。
			t.Fatalf("%s published lookup_key: %s", path, body)
		}
		// DTO 结构也不能发布内部 base_url 字段。
		if strings.Contains(body, "base_url") || strings.Contains(body, baseURL) {
			// 输出 body 定位内部 endpoint 泄漏。
			t.Fatalf("%s published base_url: %s", path, body)
		}
		// auth-index 继续按现有敏感标识规则脱敏，而不是改用 LookupKey。
		if !strings.Contains(body, `"identity":"`+helper.RedactSensitiveValue(authIndex)+`"`) {
			// 输出 body 定位 identity 字段来源错误。
			t.Fatalf("%s did not redact auth-index: %s", path, body)
		}
		// 独立 name/provider/prefix/type 必须正常发布，证明安全过滤没有删除业务 metadata。
		for _, expected := range []string{`"name":"Visible Provider"`, `"provider":"Visible Provider"`, `"prefix":"provider-prefix-visible"`, `"type":"xai"`} {
			// 每个可展示字段都必须存在。
			if !strings.Contains(body, expected) {
				// 输出 path、期望片段和 body 定位映射错误。
				t.Fatalf("%s missing %s: %s", path, expected, body)
			}
		}
	}
}

// openProviderMetadataSecretDatabase 创建真实 API/service/repository 共用的临时 SQLite。
func openProviderMetadataSecretDatabase(t *testing.T) *gorm.DB {
	// helper 失败应报告调用测试位置。
	t.Helper()
	// dbPath 放在测试临时目录。
	dbPath := filepath.Join(t.TempDir(), "provider-metadata-secret.db")
	// db 使用生产 OpenDatabase 建立完整 schema。
	db, err := repository.OpenDatabase(config.Config{SQLitePath: dbPath})
	// 数据库初始化必须成功。
	if err != nil {
		// 输出初始化错误。
		t.Fatalf("open provider metadata secret database: %v", err)
	}
	// sqlDB 用于测试结束时关闭连接。
	sqlDB, err := db.DB()
	// 底层连接必须可用。
	if err != nil {
		// 输出连接错误。
		t.Fatalf("load provider metadata secret sql database: %v", err)
	}
	// 测试结束时关闭临时连接。
	t.Cleanup(func() {
		// 关闭失败表示资源清理异常。
		if err := sqlDB.Close(); err != nil {
			// 报告清理错误。
			t.Fatalf("close provider metadata secret database: %v", err)
		}
	})
	// 返回真实数据库连接。
	return db
}
