package test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
)

func TestErrorEventServiceStoresEveryCurrentPayloadField(t *testing.T) {
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "error-events.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
	})

	receivedAt := time.Date(2026, 8, 20, 12, 1, 0, 0, time.Local)
	payload := `{
		"timestamp":"2026-08-20T12:00:00+08:00",
		"provider":"codex",
		"model":"gpt-5.6",
		"auth_id":"runtime-auth-id",
		"auth_index":"stable-auth-index",
		"status_code":429,
		"body":"quota exceeded",
		"code":"rate_limit",
		"retryable":true,
		"auth_status":{
			"status":"error",
			"status_message":"credential unavailable",
			"disabled":false,
			"unavailable":true,
			"next_retry_after":"2026-08-20T12:05:00+08:00",
			"quota":{"exceeded":true,"reason":"minute limit","next_recover_at":"2026-08-20T12:10:00+08:00","backoff_level":2},
			"model":{"name":"gpt-5.6","status":"error","status_message":"model cooling down","unavailable":true,"next_retry_after":"2026-08-20T12:03:00+08:00","quota":{"exceeded":true,"reason":"model limit","next_recover_at":"2026-08-20T12:04:00+08:00","backoff_level":1}}
		}
	}`

	provider := service.NewErrorEventService(db)
	if err := provider.StoreErrorEvent(context.Background(), payload, receivedAt); err != nil {
		t.Fatalf("StoreErrorEvent returned error: %v", err)
	}

	var row entities.ErrorEvent
	if err := db.First(&row).Error; err != nil {
		t.Fatalf("read stored error event: %v", err)
	}
	if row.Provider != "codex" || row.Model != "gpt-5.6" || row.AuthID != "runtime-auth-id" || row.AuthIndex != "stable-auth-index" {
		t.Fatalf("top-level identity fields were not preserved: %+v", row)
	}
	if !row.Timestamp.Equal(parseErrorEventTestTime(t, "2026-08-20T12:00:00+08:00")) {
		t.Fatalf("timestamp = %s", row.Timestamp)
	}
	if row.StatusCode != 429 || row.Body != "quota exceeded" || row.Code != "rate_limit" || !row.Retryable {
		t.Fatalf("top-level error fields were not preserved: %+v", row)
	}
	if row.AuthStatus != "error" || row.AuthStatusMessage != "credential unavailable" || row.AuthDisabled || !row.AuthUnavailable || row.AuthNextRetryAfter == nil || !row.AuthNextRetryAfter.Equal(parseErrorEventTestTime(t, "2026-08-20T12:05:00+08:00")) {
		t.Fatalf("auth status fields were not preserved: %+v", row)
	}
	if row.AuthQuotaExceeded == nil || !*row.AuthQuotaExceeded || row.AuthQuotaReason == nil || *row.AuthQuotaReason != "minute limit" || row.AuthQuotaNextRecoverAt == nil || !row.AuthQuotaNextRecoverAt.Equal(parseErrorEventTestTime(t, "2026-08-20T12:10:00+08:00")) || row.AuthQuotaBackoffLevel == nil || *row.AuthQuotaBackoffLevel != 2 {
		t.Fatalf("auth quota fields were not preserved: %+v", row)
	}
	if row.AuthModelName == nil || *row.AuthModelName != "gpt-5.6" || row.AuthModelStatus == nil || *row.AuthModelStatus != "error" || row.AuthModelStatusMessage == nil || *row.AuthModelStatusMessage != "model cooling down" || row.AuthModelUnavailable == nil || !*row.AuthModelUnavailable || row.AuthModelNextRetryAfter == nil || !row.AuthModelNextRetryAfter.Equal(parseErrorEventTestTime(t, "2026-08-20T12:03:00+08:00")) {
		t.Fatalf("auth model fields were not preserved: %+v", row)
	}
	if row.AuthModelQuotaExceeded == nil || !*row.AuthModelQuotaExceeded || row.AuthModelQuotaReason == nil || *row.AuthModelQuotaReason != "model limit" || row.AuthModelQuotaNextRecoverAt == nil || !row.AuthModelQuotaNextRecoverAt.Equal(parseErrorEventTestTime(t, "2026-08-20T12:04:00+08:00")) || row.AuthModelQuotaBackoffLevel == nil || *row.AuthModelQuotaBackoffLevel != 1 {
		t.Fatalf("auth model quota fields were not preserved: %+v", row)
	}
	if !row.ReceivedAt.Equal(receivedAt) {
		t.Fatalf("received_at = %s, want %s", row.ReceivedAt, receivedAt)
	}
}

func TestErrorEventServicePreservesAbsentOptionalSnapshotsAsNull(t *testing.T) {
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "error-events-null.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
	})

	payload := `{"timestamp":"2026-08-20T12:00:00+08:00","auth_index":"stable-auth-index","status_code":500,"body":"failed","auth_status":{"status":"error","disabled":false,"unavailable":false}}`
	if err := service.NewErrorEventService(db).StoreErrorEvent(context.Background(), payload, time.Now()); err != nil {
		t.Fatalf("StoreErrorEvent returned error: %v", err)
	}

	var row entities.ErrorEvent
	if err := db.First(&row).Error; err != nil {
		t.Fatalf("read stored error event: %v", err)
	}
	if row.AuthQuotaExceeded != nil || row.AuthModelName != nil || row.AuthModelQuotaExceeded != nil {
		t.Fatalf("absent nested snapshots must remain null: %+v", row)
	}
}

func TestErrorEventServiceReturnsIdentityAPIKeyForResponseRedaction(t *testing.T) {
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "error-events-list.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
	})

	identity := entities.UsageIdentity{
		AuthType:     entities.UsageIdentityAuthTypeAIProvider,
		AuthTypeName: "apikey",
		Identity:     "stable-auth-index",
		LookupKey:    "short-key",
		Name:         "Test Provider",
	}
	if err := db.Create(&identity).Error; err != nil {
		t.Fatalf("create usage identity: %v", err)
	}

	result, err := service.NewErrorEventService(db).ListErrorEvents(context.Background(), service.ErrorEventListRequest{IdentityID: identity.ID})
	if err != nil {
		t.Fatalf("ListErrorEvents returned error: %v", err)
	}
	if result.APIKey != "short-key" {
		t.Fatalf("APIKey = %q, want the identity lookup key", result.APIKey)
	}
}

func parseErrorEventTestTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("parse test time %q: %v", value, err)
	}
	return parsed
}
