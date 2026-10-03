package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/timeutil"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const readOnlyCredentialSetting = "auth.read_only.credential_hash"

type readOnlyCredentialStore interface {
	syncReadOnlyCredential(string, bool) (bool, error)
}

// BindReadOnlyPassword keeps sessions across restarts, but revokes the read-only
// role when the configured password changes or the role is disabled.
func (m *SessionManager) BindReadOnlyPassword(password string) error {
	digest := sha256.Sum256([]byte(password))
	fingerprint := hex.EncodeToString(digest[:])
	m.mu.Lock()
	defer m.mu.Unlock()
	changed := m.readOnlyCredential != "" && m.readOnlyCredential != fingerprint
	if m.store != nil {
		store, ok := m.store.(readOnlyCredentialStore)
		if !ok {
			return fmt.Errorf("session store cannot persist read-only credential binding")
		}
		var err error
		changed, err = store.syncReadOnlyCredential(fingerprint, password == "")
		if err != nil {
			return err
		}
	}
	if changed || password == "" {
		for token, session := range m.sessions {
			if session.Role == RoleReadOnly {
				delete(m.sessions, token)
				delete(m.pendingActivity, token)
			}
		}
	}
	m.readOnlyCredential = fingerprint
	return nil
}

func (s *GormSessionStore) syncReadOnlyCredential(fingerprint string, disabled bool) (bool, error) {
	if s == nil || s.db == nil {
		return false, fmt.Errorf("auth session store is not configured")
	}
	changed := false
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var setting entities.AppSetting
		err := tx.Where("setting_key = ?", readOnlyCredentialSetting).First(&setting).Error
		missing := errors.Is(err, gorm.ErrRecordNotFound)
		if err != nil && !missing {
			return err
		}
		if !missing && setting.Value != nil && bcrypt.CompareHashAndPassword([]byte(*setting.Value), []byte(fingerprint)) == nil {
			return nil
		}
		// Without a persisted binding, the previous password is unknown. Revoke
		// unbound sessions as well as sessions from a changed/disabled password.
		changed = true
		if changed {
			if err := tx.Unscoped().Where("role = ?", string(RoleReadOnly)).Delete(&entities.AuthSession{}).Error; err != nil {
				return err
			}
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(fingerprint), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		value := string(hash)
		if missing {
			return tx.Create(&entities.AppSetting{SettingKey: readOnlyCredentialSetting, Value: &value, ValueType: "string", CreatedAt: time.Now(), UpdatedAt: time.Now()}).Error
		}
		return tx.Model(&setting).Updates(map[string]any{"value": value, "updated_at": timeutil.FormatStorageTime(time.Now())}).Error
	})
	return changed, err
}
