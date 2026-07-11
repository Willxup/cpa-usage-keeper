package test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/cpa/dto/authfiles"
	"cpa-usage-keeper/internal/cpa/dto/response"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"

	"gorm.io/gorm"
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

	db := openXAIUserIDSyncDatabase(t)
	fetcher := &xaiUserIDMetadataFetcher{files: files}
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

func TestSyncMetadataClearsXAIUserIDAfterSuccessfulResponseOmitsIt(t *testing.T) {
	db := openXAIUserIDSyncDatabase(t)
	fetcher := &xaiUserIDMetadataFetcher{files: []authfiles.AuthFile{
		decodeXAIAuthFile(t, "xai-auth", "xai", `{"sub":"xai-user-old"}`),
	}}
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

	fetcher.files = []authfiles.AuthFile{{AuthIndex: "xai-auth", Type: "xai"}}
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
	db := openXAIUserIDSyncDatabase(t)
	fetcher := &xaiUserIDMetadataFetcher{files: []authfiles.AuthFile{
		decodeXAIAuthFile(t, "xai-auth", "xai", `{"sub":"xai-user-stable"}`),
	}}
	syncer := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{
		BaseURL:         "https://cpa.example.com",
		MetadataFetcher: fetcher,
	})
	if err := syncer.SyncMetadata(context.Background()); err != nil {
		t.Fatalf("initial SyncMetadata returned error: %v", err)
	}

	fetcher.files = nil
	fetcher.authErr = errors.New("auth files unavailable")
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

type xaiUserIDMetadataFetcher struct {
	files   []authfiles.AuthFile
	authErr error
}

func (f *xaiUserIDMetadataFetcher) FetchAuthFiles(context.Context) (*response.AuthFilesResult, error) {
	if f.authErr != nil {
		return nil, f.authErr
	}
	return &response.AuthFilesResult{StatusCode: 200, Payload: authfiles.AuthFilesResponse{Files: f.files}}, nil
}

func (*xaiUserIDMetadataFetcher) FetchManagementAPIKeys(context.Context) (*response.ManagementAPIKeysResult, error) {
	return &response.ManagementAPIKeysResult{StatusCode: 200}, nil
}

func (*xaiUserIDMetadataFetcher) FetchGeminiAPIKeys(context.Context) (*response.ProviderKeyConfigResult, error) {
	return &response.ProviderKeyConfigResult{StatusCode: 200}, nil
}

func (*xaiUserIDMetadataFetcher) FetchClaudeAPIKeys(context.Context) (*response.ProviderKeyConfigResult, error) {
	return &response.ProviderKeyConfigResult{StatusCode: 200}, nil
}

func (*xaiUserIDMetadataFetcher) FetchCodexAPIKeys(context.Context) (*response.ProviderKeyConfigResult, error) {
	return &response.ProviderKeyConfigResult{StatusCode: 200}, nil
}

func (*xaiUserIDMetadataFetcher) FetchVertexAPIKeys(context.Context) (*response.ProviderKeyConfigResult, error) {
	return &response.ProviderKeyConfigResult{StatusCode: 200}, nil
}

func (*xaiUserIDMetadataFetcher) FetchOpenAICompatibility(context.Context) (*response.OpenAICompatibilityResult, error) {
	return &response.OpenAICompatibilityResult{StatusCode: 200}, nil
}

func openXAIUserIDSyncDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "xai-user-id-sync.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("load sql database: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Fatalf("close sql database: %v", err)
		}
	})
	return db
}
