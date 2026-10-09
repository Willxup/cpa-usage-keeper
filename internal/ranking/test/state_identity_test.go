package test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/ranking"
	"cpa-usage-keeper/internal/repository"
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
	db, writeSQL := openRankingDatabasePools(t)
	store := ranking.NewStore(db)
	seedActiveState(t, store, 0)
	want, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("load seeded ranking state: %v", err)
	}
	heldWriter, err := writeSQL.Conn(context.Background())
	if err != nil {
		t.Fatalf("occupy writer connection: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var workers sync.WaitGroup
	defer func() {
		cancel()
		_ = heldWriter.Close()
		done := make(chan struct{})
		go func() { workers.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("ranking store operations did not stop during cleanup")
		}
	}()
	waitCountBefore := writeSQL.Stats().WaitCount
	workers.Go(func() {
		_, err := store.Update(ctx, func(next *ranking.State) error {
			next.LastError = "updated"
			return nil
		})
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("Update: %v", err)
		}
	})
	deadline := time.Now().Add(time.Second)
	for writeSQL.Stats().WaitCount == waitCountBefore {
		if time.Now().After(deadline) {
			t.Fatal("Update did not begin waiting for the occupied writer")
		}
		time.Sleep(10 * time.Millisecond)
	}
	type loadResult struct {
		state ranking.State
		err   error
	}
	loadDone := make(chan loadResult, 1)
	workers.Go(func() {
		state, err := store.Load(ctx)
		loadDone <- loadResult{state, err}
	})
	select {
	case result := <-loadDone:
		if result.err != nil {
			t.Fatalf("Load while Update was blocked: %v", result.err)
		}
		if !reflect.DeepEqual(result.state, want) {
			t.Fatalf("Load = %+v, want %+v", result.state, want)
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
	want := strings.Join([]string{
		"keeper-ranking-center/v1", "POST", "/api/v1/reports", "participant:abc_123", "42",
		"idem_1234567890ab", "2026-07-14T02:03:04.5Z", "JZQ2bWDz8799F_fImZfUuW2lIbaIk0nL_GTGfAqfnYQ",
	}, "\n")
	if string(payload) != want {
		t.Fatalf("canonical payload = %q, want %q", payload, want)
	}
}
