package test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"cpa-usage-keeper/internal/cpa/dto/authfiles"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
)

func TestSyncMetadataWritesXAIUserIDCandidatesOnlyForXAI(t *testing.T) {
	tests := []struct {
		authIndex string
		claims    string
		want      string
	}{
		{authIndex: "top-sub", claims: `{"sub":" top-sub ","subject":"lower"}`, want: "top-sub"},
		{authIndex: "top-subject", claims: `{"sub":" ","subject":" top-subject ","user_id":"lower"}`, want: "top-subject"},
		{authIndex: "top-user-id", claims: `{"user_id":" top-user-id ","userId":"lower"}`, want: "top-user-id"},
		{authIndex: "top-user-id-camel", claims: `{"userId":" top-user-id-camel ","metadata":{"sub":"lower"}}`, want: "top-user-id-camel"},
		{authIndex: "metadata-sub", claims: `{"metadata":{"sub":" metadata-sub ","subject":"lower"}}`, want: "metadata-sub"},
		{authIndex: "metadata-subject", claims: `{"metadata":{"subject":" metadata-subject ","user_id":"lower"}}`, want: "metadata-subject"},
		{authIndex: "metadata-user-id", claims: `{"metadata":{"user_id":" metadata-user-id ","userId":"lower"}}`, want: "metadata-user-id"},
		{authIndex: "metadata-user-id-camel", claims: `{"metadata":{"userId":" metadata-user-id-camel "},"attributes":{"sub":"lower"}}`, want: "metadata-user-id-camel"},
		{authIndex: "attributes-sub", claims: `{"attributes":{"sub":" attributes-sub ","subject":"lower"}}`, want: "attributes-sub"},
		{authIndex: "attributes-subject", claims: `{"attributes":{"subject":" attributes-subject ","user_id":"lower"}}`, want: "attributes-subject"},
		{authIndex: "attributes-user-id", claims: `{"attributes":{"user_id":" attributes-user-id ","userId":"lower"}}`, want: "attributes-user-id"},
		{authIndex: "attributes-user-id-camel", claims: `{"attributes":{"userId":" attributes-user-id-camel "},"oauth":{"sub":"lower"}}`, want: "attributes-user-id-camel"},
		{authIndex: "oauth-sub", claims: `{"oauth":{"sub":" oauth-sub ","subject":"lower"}}`, want: "oauth-sub"},
		{authIndex: "oauth-subject", claims: `{"oauth":{"subject":" oauth-subject "},"user":{"sub":"lower"}}`, want: "oauth-subject"},
		{authIndex: "user-sub", claims: `{"user":{"sub":" user-sub ","id":"lower"}}`, want: "user-sub"},
		{authIndex: "user-id", claims: `{"user":{"id":" user-id "}}`, want: "user-id"},
		{authIndex: "metadata-oauth", claims: `{"metadata":{"oauth":{"sub":" metadata-oauth-sub "}},"attributes":{"oauth":{"sub":"lower"}}}`, want: "metadata-oauth-sub"},
		{authIndex: "attributes-oauth", claims: `{"attributes":{"oauth":{"subject":" attributes-oauth-subject "}},"user":{"sub":"lower"}}`, want: "attributes-oauth-subject"},
		{authIndex: "metadata-user", claims: `{"metadata":{"user":{"sub":" metadata-user-sub "}},"attributes":{"user":{"sub":"lower"}}}`, want: "metadata-user-sub"},
		{authIndex: "attributes-user", claims: `{"attributes":{"user":{"id":" attributes-user-id "}}}`, want: "attributes-user-id"},
		{authIndex: "numeric-sub", claims: `{"sub":12345,"subject":"lower"}`, want: "12345"},
	}

	files := make([]authfiles.AuthFile, 0, len(tests)+1)
	for _, tt := range tests {
		file := decodeXAIAuthFile(t, tt.authIndex, "xai", tt.claims)
		files = append(files, file)
	}
	files = append(files, decodeXAIAuthFile(t, "non-xai", "claude", `{"sub":"must-not-sync"}`))

	// 使用共享 metadata 数据库 helper，避免保留第二套测试装配。
	db := openMetadataTestDatabase(t, "xai-user-id-candidates.db")
	// 共享 fetcher 默认覆盖全部 provider endpoint。
	fetcher := newMetadataTestFetcher()
	// 当前测试只替换 Auth Files payload。
	fetcher.setAuthFiles(files)
	syncer := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{
		BaseURL:         "https://cpa.example.com",
		MetadataFetcher: fetcher,
	})
	if err := syncer.SyncMetadata(context.Background()); err != nil {
		t.Fatalf("SyncMetadata returned error: %v", err)
	}

	for _, tt := range tests {
		row, err := repository.GetActiveAuthFileUsageIdentityByAuthIndex(context.Background(), db, tt.authIndex)
		if err != nil {
			t.Fatalf("read %s usage identity: %v", tt.authIndex, err)
		}
		if row.XAIUserID == nil || *row.XAIUserID != tt.want {
			t.Fatalf("expected %s xAI user id %q, got %+v", tt.authIndex, tt.want, row.XAIUserID)
		}
	}
	nonXAI, err := repository.GetActiveAuthFileUsageIdentityByAuthIndex(context.Background(), db, "non-xai")
	if err != nil {
		t.Fatalf("read non-xAI usage identity: %v", err)
	}
	if nonXAI.XAIUserID != nil {
		t.Fatalf("expected non-xAI auth file to ignore xAI user id candidates, got %q", *nonXAI.XAIUserID)
	}
}

func TestSyncMetadataMirrorsXAINullishNestedRecordFallback(t *testing.T) {
	tests := []struct {
		authIndex string
		claims    string
		want      string
	}{
		{authIndex: "null-oauth-fallback", claims: `{"oauth":null,"metadata":{"oauth":{"sub":"metadata-oauth"}}}`, want: "metadata-oauth"},
		{authIndex: "invalid-oauth-blocks", claims: `{"oauth":false,"metadata":{"oauth":{"sub":"must-not-fallback"}}}`},
		{authIndex: "invalid-metadata-oauth-blocks", claims: `{"metadata":{"oauth":[]},"attributes":{"oauth":{"sub":"must-not-fallback"}}}`},
		{authIndex: "null-user-fallback", claims: `{"user":null,"metadata":{"user":{"id":"metadata-user"}}}`, want: "metadata-user"},
		{authIndex: "invalid-user-blocks", claims: `{"user":123,"metadata":{"user":{"id":"must-not-fallback"}}}`},
	}

	files := make([]authfiles.AuthFile, 0, len(tests))
	for _, tt := range tests {
		files = append(files, decodeXAIAuthFile(t, tt.authIndex, "xai", tt.claims))
	}
	// 使用共享 metadata 数据库 helper。
	db := openMetadataTestDatabase(t, "xai-nullish-fallback.db")
	// 共享 fetcher 避免为 XAI 测试复制七个 provider 方法。
	fetcher := newMetadataTestFetcher()
	// 当前测试只替换 Auth Files payload。
	fetcher.setAuthFiles(files)
	syncer := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{
		BaseURL:         "https://cpa.example.com",
		MetadataFetcher: fetcher,
	})
	if err := syncer.SyncMetadata(context.Background()); err != nil {
		t.Fatalf("SyncMetadata returned error: %v", err)
	}

	for _, tt := range tests {
		row, err := repository.GetActiveAuthFileUsageIdentityByAuthIndex(context.Background(), db, tt.authIndex)
		if err != nil {
			t.Fatalf("read %s usage identity: %v", tt.authIndex, err)
		}
		if tt.want == "" {
			if row.XAIUserID != nil {
				t.Fatalf("expected invalid %s container to block fallback, got %q", tt.authIndex, *row.XAIUserID)
			}
			continue
		}
		if row.XAIUserID == nil || *row.XAIUserID != tt.want {
			t.Fatalf("expected %s fallback %q, got %+v", tt.authIndex, tt.want, row.XAIUserID)
		}
	}
}

func TestSyncMetadataClearsXAIUserIDAfterSuccessfulResponseOmitsIt(t *testing.T) {
	// 使用共享 metadata 数据库 helper。
	db := openMetadataTestDatabase(t, "xai-clear-user-id.db")
	// 共享 fetcher 支持同一实例连续两轮替换 Auth Files。
	fetcher := newMetadataTestFetcher()
	// 首轮写入带 user id 的 xAI Auth File。
	fetcher.setAuthFiles([]authfiles.AuthFile{
		decodeXAIAuthFile(t, "xai-auth", "xai", `{"sub":"xai-user-old"}`),
	})
	syncer := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{
		BaseURL:         "https://cpa.example.com",
		MetadataFetcher: fetcher,
	})
	if err := syncer.SyncMetadata(context.Background()); err != nil {
		t.Fatalf("initial SyncMetadata returned error: %v", err)
	}
	initial, err := repository.GetActiveAuthFileUsageIdentityByAuthIndex(context.Background(), db, "xai-auth")
	if err != nil {
		t.Fatalf("read initial xAI usage identity: %v", err)
	}
	if initial.XAIUserID == nil || *initial.XAIUserID != "xai-user-old" {
		t.Fatalf("expected initial xAI user id to persist, got %+v", initial.XAIUserID)
	}

	// 第二轮成功响应省略候选字段，必须清空旧 XAIUserID。
	fetcher.setAuthFiles([]authfiles.AuthFile{{AuthIndex: "xai-auth", Type: "xai"}})
	if err := syncer.SyncMetadata(context.Background()); err != nil {
		t.Fatalf("clearing SyncMetadata returned error: %v", err)
	}
	row, err := repository.GetActiveAuthFileUsageIdentityByAuthIndex(context.Background(), db, "xai-auth")
	if err != nil {
		t.Fatalf("read cleared xAI usage identity: %v", err)
	}
	if row.XAIUserID != nil {
		t.Fatalf("expected successful sync without a candidate to clear xAI user id, got %q", *row.XAIUserID)
	}
}

func TestSyncMetadataPreservesXAIUserIDWhenAuthFilesFetchFails(t *testing.T) {
	// 使用共享 metadata 数据库 helper。
	db := openMetadataTestDatabase(t, "xai-preserve-on-error.db")
	// 共享 fetcher 先返回成功 Auth Files。
	fetcher := newMetadataTestFetcher()
	// 首轮建立带稳定 user id 的 identity。
	fetcher.setAuthFiles([]authfiles.AuthFile{
		decodeXAIAuthFile(t, "xai-auth", "xai", `{"sub":"xai-user-stable"}`),
	})
	syncer := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{
		BaseURL:         "https://cpa.example.com",
		MetadataFetcher: fetcher,
	})
	if err := syncer.SyncMetadata(context.Background()); err != nil {
		t.Fatalf("initial SyncMetadata returned error: %v", err)
	}

	// 失败轮不提供可用 Auth Files result。
	fetcher.authFilesResult = nil
	// 独立 fetch error 必须保留首轮 XAIUserID。
	fetcher.authFilesErr = errors.New("auth files unavailable")
	if err := syncer.SyncMetadata(context.Background()); err == nil {
		t.Fatal("expected failed auth files fetch to return an error")
	}
	row, err := repository.GetActiveAuthFileUsageIdentityByAuthIndex(context.Background(), db, "xai-auth")
	if err != nil {
		t.Fatalf("read preserved xAI usage identity: %v", err)
	}
	if row.XAIUserID == nil || *row.XAIUserID != "xai-user-stable" {
		t.Fatalf("expected failed fetch to preserve xAI user id, got %+v", row.XAIUserID)
	}
}

func decodeXAIAuthFile(t *testing.T, authIndex, authType, claims string) authfiles.AuthFile {
	t.Helper()
	var file authfiles.AuthFile
	if err := json.Unmarshal([]byte(claims), &file); err != nil {
		t.Fatalf("decode %s claims: %v", authIndex, err)
	}
	file.AuthIndex = authIndex
	file.Type = authType
	return file
}
