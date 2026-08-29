package test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/ranking"
	"cpa-usage-keeper/internal/repository"
	"gorm.io/gorm"
)

func TestRankingStoreDefaultsToDisabledWithoutPersistedIdentity(t *testing.T) {
	store := ranking.NewStore(openRankingDatabase(t))

	state, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if state.Status != ranking.StatusDisabled {
		t.Fatalf("expected disabled state, got %+v", state)
	}
	if state.PublicKey != "" || state.PrivateKey != "" || state.ParticipantID != "" {
		t.Fatalf("expected empty identity before joining, got %+v", state)
	}
}

func TestRankingStorePersistsJoiningIdentityAndProtectsDeletedTombstone(t *testing.T) {
	store := ranking.NewStore(openRankingDatabase(t))
	state := ranking.State{
		Status:                     ranking.StatusJoining,
		PublicKey:                  base64.RawURLEncoding.EncodeToString(make([]byte, ed25519.PublicKeySize)),
		PrivateKey:                 base64.RawURLEncoding.EncodeToString(make([]byte, ed25519.PrivateKeySize)),
		RegistrationIdempotencyKey: "registration_once",
		DisplayName:                "Keeper_01",
		AvatarID:                   7,
	}
	if err := store.Save(context.Background(), state); err != nil {
		t.Fatalf("Save joining state returned error: %v", err)
	}

	loaded, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load joining state returned error: %v", err)
	}
	if !reflect.DeepEqual(loaded, state) {
		t.Fatalf("loaded state mismatch:\nwant=%+v\n got=%+v", state, loaded)
	}

	loaded.Status = ranking.StatusDeleted
	if err := store.Save(context.Background(), loaded); err != nil {
		t.Fatalf("Save deleted state returned error: %v", err)
	}
	loaded.Status = ranking.StatusActive
	loaded.ParticipantID = "p_should_not_reactivate"
	startedAt := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	loaded.ParticipationStartedAt = &startedAt
	if err := store.Save(context.Background(), loaded); !errors.Is(err, ranking.ErrDeletedState) {
		t.Fatalf("expected deleted tombstone protection, got %v", err)
	}
}

func TestRankingStorePersistsPausedActiveIdentity(t *testing.T) {
	store := ranking.NewStore(openRankingDatabase(t))
	seedActiveState(t, store, 3)
	state, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("load active state: %v", err)
	}
	state.Status = ranking.StatusPaused
	if err := store.Save(context.Background(), state); err != nil {
		t.Fatalf("save paused state: %v", err)
	}
	loaded, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("load paused state: %v", err)
	}
	if !reflect.DeepEqual(loaded, state) {
		t.Fatalf("paused state mismatch:\nwant=%+v\n got=%+v", state, loaded)
	}
}

func TestRankingStoreRejectsUnknownPersistedFields(t *testing.T) {
	db := openRankingDatabase(t)
	value := `{"status":"disabled","unexpected":true}`
	if _, err := repository.UpsertAppSetting(context.Background(), db, entities.AppSetting{
		SettingKey: ranking.IdentitySettingKey,
		Value:      &value,
		ValueType:  entities.AppSettingValueTypeJSON,
	}); err != nil {
		t.Fatalf("seed malformed ranking state: %v", err)
	}

	if _, err := ranking.NewStore(db).Load(context.Background()); err == nil {
		t.Fatal("expected unknown persisted field to be rejected")
	}
}

func TestRankingStoreRejectsUnsupportedPersistedProfile(t *testing.T) {
	identity, err := ranking.GenerateIdentity(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateIdentity returned error: %v", err)
	}
	err = ranking.NewStore(openRankingDatabase(t)).Save(context.Background(), ranking.State{
		Status: ranking.StatusJoining, PublicKey: identity.PublicKey, PrivateKey: identity.PrivateKey,
		RegistrationIdempotencyKey: "registration_once", DisplayName: "Bad.Name", AvatarID: 7,
	})
	if !errors.Is(err, ranking.ErrInvalidState) {
		t.Fatalf("expected unsupported persisted profile to be rejected, got %v", err)
	}
}

func TestRankingStoreLoadBypassesBlockedUpdate(t *testing.T) {
	db, reader, err := repository.OpenDatabasePools(config.Config{SQLitePath: filepath.Join(t.TempDir(), "ranking-store.db")})
	if err != nil {
		t.Fatalf("OpenDatabasePools returned error: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		if sqlDB, err := reader.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	identity, err := ranking.GenerateIdentity(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateIdentity returned error: %v", err)
	}
	startedAt := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	want := ranking.State{
		Status:                     ranking.StatusActive,
		PublicKey:                  identity.PublicKey,
		PrivateKey:                 identity.PrivateKey,
		RegistrationIdempotencyKey: "registration_once",
		DisplayName:                "Keeper_01",
		AvatarID:                   7,
		ParticipantID:              "p_example",
		ParticipationStartedAt:     &startedAt,
	}
	store := ranking.NewStore(db)
	if err := store.Save(context.Background(), want); err != nil {
		t.Fatalf("seed active ranking state: %v", err)
	}

	writeSQL, err := db.DB()
	if err != nil {
		t.Fatalf("load writer pool: %v", err)
	}
	heldWriter, err := writeSQL.Conn(context.Background())
	if err != nil {
		t.Fatalf("occupy writer connection: %v", err)
	}
	writerHeld := true

	updateCtx, cancelUpdate := context.WithCancel(context.Background())
	updateDone := make(chan error, 1)
	waitCountBefore := writeSQL.Stats().WaitCount
	go func() {
		_, updateErr := store.Update(updateCtx, func(next *ranking.State) error {
			next.LastError = "updated"
			return nil
		})
		updateDone <- updateErr
	}()

	type loadResult struct {
		state ranking.State
		err   error
	}
	loadDone := make(chan loadResult, 1)
	loadStarted := false
	loadCtx, cancelLoad := context.WithCancel(context.Background())
	defer func() {
		cancelLoad()
		cancelUpdate()
		if writerHeld {
			_ = heldWriter.Close()
		}
		select {
		case <-updateDone:
		case <-time.After(2 * time.Second):
			t.Errorf("blocked Update did not stop during cleanup")
		}
		if loadStarted {
			select {
			case <-loadDone:
			case <-time.After(2 * time.Second):
				t.Errorf("Load did not stop during cleanup")
			}
		}
	}()

	deadline := time.Now().Add(time.Second)
	for writeSQL.Stats().WaitCount == waitCountBefore {
		if time.Now().After(deadline) {
			t.Fatal("Update did not begin waiting for the occupied writer")
		}
		time.Sleep(10 * time.Millisecond)
	}

	loadStarted = true
	go func() {
		state, loadErr := store.Load(loadCtx)
		loadDone <- loadResult{state: state, err: loadErr}
	}()

	select {
	case result := <-loadDone:
		loadStarted = false
		if result.err != nil {
			t.Fatalf("Load returned error while Update was blocked: %v", result.err)
		}
		if !reflect.DeepEqual(result.state, want) {
			t.Fatalf("Load returned an unexpected state:\nwant=%+v\n got=%+v", want, result.state)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("Load waited for the blocked Update instead of using the reader pool")
	}
}

func TestRankingIdentitySignsCenterCanonicalPayload(t *testing.T) {
	identity, err := ranking.GenerateIdentity(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateIdentity returned error: %v", err)
	}
	sequence := int64(8)
	timestamp := time.Date(2026, 7, 24, 2, 3, 4, 5, time.UTC)
	body := []byte(`{"metrics_version":1}`)
	input := ranking.SigningInput{
		Method:         http.MethodPost,
		Path:           "/api/v1/reports",
		Subject:        "participant:p_example",
		Sequence:       &sequence,
		IdempotencyKey: "report_once",
		Timestamp:      timestamp,
	}

	payload, err := ranking.CanonicalSigningPayload(input, body)
	if err != nil {
		t.Fatalf("CanonicalSigningPayload returned error: %v", err)
	}
	signature, err := identity.Sign(payload)
	if err != nil {
		t.Fatalf("Sign returned error: %v", err)
	}
	publicKey, err := base64.RawURLEncoding.DecodeString(identity.PublicKey)
	if err != nil {
		t.Fatalf("decode public key: %v", err)
	}
	if !ed25519.Verify(ed25519.PublicKey(publicKey), payload, signature) {
		t.Fatal("expected generated signature to verify")
	}
}

func TestRankingCanonicalPayloadMatchesCenterContract(t *testing.T) {
	sequence := int64(42)
	timestamp := time.Date(2026, 7, 14, 2, 3, 4, 500_000_000, time.UTC)
	body := []byte(`{"snapshot_at":"2026-07-14T02:03:04.5Z"}`)
	payload, err := ranking.CanonicalSigningPayload(ranking.SigningInput{
		Method: http.MethodPost, Path: "/api/v1/reports", Subject: "participant:abc_123",
		Sequence: &sequence, IdempotencyKey: "idem_1234567890ab", Timestamp: timestamp,
	}, body)
	if err != nil {
		t.Fatalf("CanonicalSigningPayload returned error: %v", err)
	}
	lines := strings.Split(string(payload), "\n")
	if len(lines) != 8 || lines[0] != "keeper-ranking-center/v1" || lines[1] != "POST" || lines[2] != "/api/v1/reports" || lines[3] != "participant:abc_123" || lines[4] != "42" || lines[5] != "idem_1234567890ab" || lines[6] != "2026-07-14T02:03:04.5Z" || lines[7] != "JZQ2bWDz8799F_fImZfUuW2lIbaIk0nL_GTGfAqfnYQ" {
		t.Fatalf("canonical payload does not match ranking-center: %q", payload)
	}
}

func openRankingDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "ranking.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}
