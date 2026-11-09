package test

import (
	"context"
	"errors"
	"testing"
	"time"

	"cpa-usage-keeper/internal/repository"
	keeperservice "cpa-usage-keeper/internal/service"

	"gorm.io/gorm"
)

func TestListCPAAPIKeysHonorsInFlightCancellation(t *testing.T) {
	db := openUsageServiceTestDatabase(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	connection, err := sqlDB.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	beforeWait := sqlDB.Stats().WaitCount
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	provider := keeperservice.NewCPAAPIKeyService(db)
	go func() { _, err := provider.ListCPAAPIKeys(ctx); done <- err }()
	finished := false
	defer func() {
		cancel()
		_ = connection.Close()
		if !finished {
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Error("key query did not finish after releasing the connection")
			}
		}
	}()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for sqlDB.Stats().WaitCount == beforeWait {
		select {
		case err := <-done:
			finished = true
			t.Fatalf("key query returned before waiting for the connection: %v", err)
		case <-deadline.C:
			t.Fatal("key query did not wait for the connection")
		case <-tick.C:
		}
	}
	cancel()
	select {
	case err := <-done:
		finished = true
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected canceled key query, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceling the caller did not stop the in-flight key query")
	}
}

func TestFindActiveCPAAPIKeyByValueTrimsInputAndQueriesActiveRow(t *testing.T) {
	db := openUsageServiceTestDatabase(t)
	if err := repository.SyncCPAAPIKeys(db, []string{"sk-alpha123456", "sk-beta123456"}, time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("seed API keys: %v", err)
	}
	provider := keeperservice.NewCPAAPIKeyService(db)

	row, err := provider.FindActiveCPAAPIKeyByValue(context.Background(), "  sk-beta123456  ")
	if err != nil {
		t.Fatalf("FindActiveCPAAPIKeyByValue returned error: %v", err)
	}
	if row.ID != 2 || row.DisplayKey == "" || row.APIKey != "sk-beta123456" {
		t.Fatalf("unexpected matched row: %+v", row)
	}
}

func TestFindActiveCPAAPIKeyByValueRejectsEmptyAndMissingAsNotFound(t *testing.T) {
	db := openUsageServiceTestDatabase(t)
	if err := repository.SyncCPAAPIKeys(db, []string{"sk-alpha123456"}, time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("seed API keys: %v", err)
	}
	provider := keeperservice.NewCPAAPIKeyService(db)

	for _, apiKey := range []string{"   ", "sk-missing"} {
		if _, err := provider.FindActiveCPAAPIKeyByValue(context.Background(), apiKey); !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatalf("expected ErrRecordNotFound for %q, got %v", apiKey, err)
		}
	}
}

func TestFindActiveCPAAPIKeyByIDReturnsOnlyActiveRows(t *testing.T) {
	db := openUsageServiceTestDatabase(t)
	if err := repository.SyncCPAAPIKeys(db, []string{"sk-alpha123456", "sk-beta123456"}, time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("seed API keys: %v", err)
	}
	if err := repository.SyncCPAAPIKeys(db, []string{"sk-alpha123456"}, time.Date(2026, 5, 13, 11, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("mark stale API key deleted: %v", err)
	}
	provider := keeperservice.NewCPAAPIKeyService(db)

	row, err := provider.FindActiveCPAAPIKeyByID(context.Background(), 1)
	if err != nil {
		t.Fatalf("FindActiveCPAAPIKeyByID active row returned error: %v", err)
	}
	if row.ID != 1 {
		t.Fatalf("expected row 1, got %+v", row)
	}
	if _, err := provider.FindActiveCPAAPIKeyByID(context.Background(), 2); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected deleted row to return ErrRecordNotFound, got %v", err)
	}
}

func TestUpdateCPAAPIKeyAliasAcceptsParsedInt64ID(t *testing.T) {
	db := openUsageServiceTestDatabase(t)
	if err := repository.SyncCPAAPIKeys(db, []string{"sk-alpha123456"}, time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("seed API keys: %v", err)
	}
	provider := keeperservice.NewCPAAPIKeyService(db)

	row, err := provider.UpdateCPAAPIKeyAlias(context.Background(), int64(1), "Primary Key")
	if err != nil {
		t.Fatalf("UpdateCPAAPIKeyAlias returned error: %v", err)
	}
	if row.ID != 1 || row.KeyAlias != "Primary Key" {
		t.Fatalf("unexpected updated row: %+v", row)
	}
}
