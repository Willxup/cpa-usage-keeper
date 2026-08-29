package ranking

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"gorm.io/gorm"
)

type Store struct {
	db *gorm.DB
	mu sync.Mutex
}

func NewStore(db *gorm.DB) *Store {
	return &Store{db: db}
}

func (s *Store) Load(ctx context.Context) (State, error) {
	if s == nil || s.db == nil {
		return State{}, fmt.Errorf("%w: ranking store is not configured", ErrInvalidState)
	}
	return loadState(ctx, s.db)
}

func (s *Store) Save(ctx context.Context, next State) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("%w: ranking store is not configured", ErrInvalidState)
	}
	if err := validateState(next); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		current, err := loadState(ctx, tx)
		if err != nil {
			return err
		}
		if current.Status == StatusDeleted && next.Status != StatusDeleted {
			return ErrDeletedState
		}
		payload, err := json.Marshal(next)
		if err != nil {
			return fmt.Errorf("encode ranking state: %w", err)
		}
		value := string(payload)
		_, err = repository.UpsertAppSetting(ctx, tx, entities.AppSetting{
			SettingKey: IdentitySettingKey,
			Value:      &value,
			ValueType:  entities.AppSettingValueTypeJSON,
		})
		if err != nil {
			return fmt.Errorf("save ranking state: %w", err)
		}
		return nil
	})
}

// Update 在同一进程内串行读改写，供序号分配和同步状态更新使用。
func (s *Store) Update(ctx context.Context, update func(*State) error) (State, error) {
	if s == nil || s.db == nil || update == nil {
		return State{}, fmt.Errorf("%w: ranking store update is not configured", ErrInvalidState)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	var saved State
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		current, err := loadState(ctx, tx)
		if err != nil {
			return err
		}
		next := current
		if err := update(&next); err != nil {
			return err
		}
		if current.Status == StatusDeleted && next.Status != StatusDeleted {
			return ErrDeletedState
		}
		if err := validateState(next); err != nil {
			return err
		}
		payload, err := json.Marshal(next)
		if err != nil {
			return fmt.Errorf("encode ranking state: %w", err)
		}
		value := string(payload)
		if _, err := repository.UpsertAppSetting(ctx, tx, entities.AppSetting{
			SettingKey: IdentitySettingKey,
			Value:      &value,
			ValueType:  entities.AppSettingValueTypeJSON,
		}); err != nil {
			return fmt.Errorf("save ranking state: %w", err)
		}
		saved = next
		return nil
	})
	return saved, err
}

func loadState(ctx context.Context, db *gorm.DB) (State, error) {
	setting, found, err := repository.GetAppSetting(ctx, db, IdentitySettingKey)
	if err != nil {
		return State{}, fmt.Errorf("load ranking state: %w", err)
	}
	if !found || setting.Value == nil || strings.TrimSpace(*setting.Value) == "" {
		return State{Status: StatusDisabled}, nil
	}
	if setting.ValueType != entities.AppSettingValueTypeJSON {
		return State{}, fmt.Errorf("%w: ranking identity must be JSON", ErrInvalidState)
	}

	var state State
	decoder := json.NewDecoder(bytes.NewBufferString(*setting.Value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return State{}, fmt.Errorf("%w: decode ranking identity: %v", ErrInvalidState, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return State{}, fmt.Errorf("%w: ranking identity must contain one JSON value", ErrInvalidState)
	}
	if err := validateState(state); err != nil {
		return State{}, err
	}
	return state, nil
}

func validateState(state State) error {
	switch state.Status {
	case StatusDisabled:
		if state != (State{Status: StatusDisabled}) {
			return fmt.Errorf("%w: disabled state cannot contain an identity", ErrInvalidState)
		}
		return nil
	case StatusJoining, StatusActive, StatusPaused, StatusDeleted:
	default:
		return fmt.Errorf("%w: unsupported status %q", ErrInvalidState, state.Status)
	}
	publicKey, err := decodeKey(state.PublicKey, ed25519.PublicKeySize)
	if err != nil {
		return fmt.Errorf("%w: invalid public key", ErrInvalidState)
	}
	privateKey, err := decodeKey(state.PrivateKey, ed25519.PrivateKeySize)
	if err != nil {
		return fmt.Errorf("%w: invalid private key", ErrInvalidState)
	}
	if !bytes.Equal(publicKey, ed25519.PrivateKey(privateKey).Public().(ed25519.PublicKey)) {
		return fmt.Errorf("%w: public and private keys do not match", ErrInvalidState)
	}
	if strings.TrimSpace(state.RegistrationIdempotencyKey) == "" || len(state.RegistrationIdempotencyKey) > 64 || strings.ContainsAny(state.RegistrationIdempotencyKey, " \t\r\n") {
		return fmt.Errorf("%w: invalid registration idempotency key", ErrInvalidState)
	}
	normalizedName, profileErr := normalizeProfile(state.DisplayName, state.AvatarID)
	if profileErr != nil || normalizedName != state.DisplayName {
		return fmt.Errorf("%w: invalid immutable profile", ErrInvalidState)
	}
	if (state.Status == StatusActive || state.Status == StatusPaused) && strings.TrimSpace(state.ParticipantID) == "" {
		return fmt.Errorf("%w: registered state requires participant_id", ErrInvalidState)
	}
	if (state.Status == StatusActive || state.Status == StatusPaused) && state.ParticipationStartedAt == nil {
		return fmt.Errorf("%w: registered state requires participation_started_at", ErrInvalidState)
	}
	if strings.ContainsAny(state.ParticipantID, " \t\r\n") || state.LastAllocatedSequence < 0 || state.LastSuccessfulCompleteEventID < 0 {
		return fmt.Errorf("%w: invalid participant state", ErrInvalidState)
	}
	if state.LastSuccessfulCompleteDay == "" && state.LastSuccessfulCompleteEventID != 0 {
		return fmt.Errorf("%w: completed event watermark requires a completed day", ErrInvalidState)
	}
	if state.LastSuccessfulCompleteDay != "" {
		parsed, err := time.Parse("2006-01-02", state.LastSuccessfulCompleteDay)
		if err != nil || parsed.Format("2006-01-02") != state.LastSuccessfulCompleteDay {
			return fmt.Errorf("%w: invalid completed day", ErrInvalidState)
		}
	}
	for _, timestamp := range []*time.Time{state.ParticipationStartedAt, state.LastSuccessfulSyncAt, state.LastAttemptAt} {
		if timestamp == nil {
			continue
		}
		_, offset := timestamp.Zone()
		if timestamp.IsZero() || offset != 0 {
			return fmt.Errorf("%w: state timestamps must use UTC", ErrInvalidState)
		}
	}
	if len(state.LastError) > 512 {
		return fmt.Errorf("%w: last error is too long", ErrInvalidState)
	}
	return nil
}

func decodeKey(encoded string, length int) ([]byte, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(decoded) != length || base64.RawURLEncoding.EncodeToString(decoded) != encoded {
		return nil, errors.New("invalid base64url key")
	}
	return decoded, nil
}
