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
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
	"gorm.io/gorm"
)

// TestProviderMetadataSecretStaysOutOfUsageIdentityResponses 验证 CPA api-key 只留在 LookupKey，不进入列表 API。
func TestProviderMetadataSecretStaysOutOfUsageIdentityResponses(t *testing.T) {
	db := openProviderMetadataSecretDatabase(t)
	secret := "unique-provider-secret-7f18c2"
	authIndex := "provider-auth-index-91a6"
	prefix := "provider-prefix-visible"
	baseURL := "https://provider-base-url.example/v1"
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	if err := repository.ReplaceUsageIdentitiesForProviderTypes(context.Background(), db, []entities.UsageIdentity{{Name: "Visible Provider", AuthTypeName: "apikey", Identity: authIndex, Type: "xai", Provider: "Visible Provider", LookupKey: secret, Prefix: prefix, BaseURL: baseURL}}, []string{"xai"}, now); err != nil {
		t.Fatalf("seed provider metadata identity: %v", err)
	}
	var stored entities.UsageIdentity
	if err := db.Where("auth_type = ? AND identity = ?", entities.UsageIdentityAuthTypeAIProvider, authIndex).First(&stored).Error; err != nil {
		t.Fatalf("load provider metadata identity: %v", err)
	}
	// 数据库内部 LookupKey 必须保留完整 secret，且其它字段保持独立值。
	if stored.LookupKey != secret || stored.Identity != authIndex || stored.Prefix != prefix || stored.BaseURL != baseURL || stored.Name != "Visible Provider" {
		t.Fatalf("stored provider metadata fields = %+v", stored)
	}
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "", OptionalProviders{UsageIdentity: service.NewUsageIdentityService(db)})
	// 同时检查旧列表与分页列表两个公开响应入口。
	for _, path := range []string{"/api/v1/usage/identities", "/api/v1/usage/identities/page?auth_type=2&page=1&page_size=10"} {
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		router.ServeHTTP(resp, req)
		body := resp.Body.String()
		if resp.Code != http.StatusOK {
			t.Fatalf("%s status = %d body=%s", path, resp.Code, body)
		}
		if strings.Contains(body, secret) {
			t.Fatalf("%s leaked provider secret: %s", path, body)
		}
		if strings.Contains(body, "lookup_key") {
			t.Fatalf("%s published lookup_key: %s", path, body)
		}
		if strings.Contains(body, "base_url") || strings.Contains(body, baseURL) {
			t.Fatalf("%s published base_url: %s", path, body)
		}
		// identity 发布原始 auth-index，供详情事件查询精确使用；它不是 LookupKey/API Key。
		if !strings.Contains(body, `"identity":"`+authIndex+`"`) {
			t.Fatalf("%s did not publish auth-index: %s", path, body)
		}
		// 独立 name/provider/prefix/type 必须正常发布，证明安全过滤没有删除业务 metadata。
		for _, expected := range []string{`"name":"Visible Provider"`, `"provider":"Visible Provider"`, `"prefix":"provider-prefix-visible"`, `"type":"xai"`} {
			if !strings.Contains(body, expected) {
				t.Fatalf("%s missing %s: %s", path, expected, body)
			}
		}
	}
}

func openProviderMetadataSecretDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "provider-metadata-secret.db")
	db, err := repository.OpenDatabase(config.Config{SQLitePath: dbPath})
	if err != nil {
		t.Fatalf("open provider metadata secret database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("load provider metadata secret sql database: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Fatalf("close provider metadata secret database: %v", err)
		}
	})
	return db
}
