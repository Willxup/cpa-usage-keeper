package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type SessionStore interface {
	Save(string, Session) error
	Get(string) (Session, bool, error)
	Delete(string) error
	DeleteExpired(time.Time) error
}

type GormSessionStore struct {
	db *gorm.DB
}

func NewGormSessionStore(db *gorm.DB) *GormSessionStore {
	return &GormSessionStore{db: db}
}

func (s *GormSessionStore) Save(token string, session Session) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("auth session store is not configured")
	}
	row := entities.AuthSession{
		TokenHash:   sessionTokenHash(token),
		Role:        string(session.Role),
		CPAAPIKeyID: session.CPAAPIKeyID,
		ExpiresAt:   session.ExpiresAt,
	}
	return s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "token_hash"}},
		UpdateAll: true,
	}).Create(&row).Error
}

func (s *GormSessionStore) Get(token string) (Session, bool, error) {
	if s == nil || s.db == nil {
		return Session{}, false, fmt.Errorf("auth session store is not configured")
	}
	var row entities.AuthSession
	if err := s.db.Where("token_hash = ?", sessionTokenHash(token)).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Session{}, false, nil
		}
		return Session{}, false, err
	}
	session, err := authSessionFromRow(row)
	if err != nil {
		return Session{}, false, err
	}
	return session, true, nil
}

func (s *GormSessionStore) Delete(token string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("auth session store is not configured")
	}
	return s.db.Where("token_hash = ?", sessionTokenHash(token)).Delete(&entities.AuthSession{}).Error
}

func (s *GormSessionStore) DeleteExpired(now time.Time) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("auth session store is not configured")
	}
	return s.db.Where("expires_at <= ?", timeutil.FormatStorageTime(now)).Delete(&entities.AuthSession{}).Error
}

func authSessionFromRow(row entities.AuthSession) (Session, error) {
	switch Role(row.Role) {
	case RoleAdmin:
		return Session{Role: RoleAdmin, ExpiresAt: row.ExpiresAt}, nil
	case RoleAPIKeyViewer:
		return Session{Role: RoleAPIKeyViewer, CPAAPIKeyID: row.CPAAPIKeyID, ExpiresAt: row.ExpiresAt}, nil
	default:
		return Session{}, fmt.Errorf("unknown auth session role %q", row.Role)
	}
}

func sessionTokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

type Role string

const (
	RoleAdmin        Role = "admin"
	RoleAPIKeyViewer Role = "api_key_viewer"
)

type Session struct {
	Role        Role
	CPAAPIKeyID int64
	ExpiresAt   time.Time
}

type SessionManager struct {
	ttl      time.Duration
	now      func() time.Time
	generate func() (string, error)
	store    SessionStore

	mu       sync.RWMutex
	sessions map[string]Session
}

func NewSessionManager(ttl time.Duration) *SessionManager {
	return &SessionManager{
		ttl:      ttl,
		now:      time.Now,
		generate: generateToken,
		sessions: make(map[string]Session),
	}
}

func NewPersistentSessionManager(ttl time.Duration, store SessionStore) *SessionManager {
	manager := NewSessionManager(ttl)
	manager.store = store
	return manager
}

func (m *SessionManager) Create() (string, time.Time, error) {
	return m.create(Session{Role: RoleAdmin})
}

func (m *SessionManager) CreateAPIKeyViewer(cpaAPIKeyID int64) (string, time.Time, error) {
	return m.create(Session{Role: RoleAPIKeyViewer, CPAAPIKeyID: cpaAPIKeyID})
}

func (m *SessionManager) create(session Session) (string, time.Time, error) {
	token, err := m.generate()
	if err != nil {
		return "", time.Time{}, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.cleanupExpiredLocked()
	expiresAt := m.now().Add(m.ttl)
	session.ExpiresAt = expiresAt
	if m.store != nil {
		if err := m.store.Save(token, session); err != nil {
			return "", time.Time{}, fmt.Errorf("save auth session: %w", err)
		}
	}
	m.sessions[token] = session

	return token, expiresAt, nil
}

func (m *SessionManager) Validate(token string) bool {
	_, ok := m.Get(token)
	return ok
}

func (m *SessionManager) Get(token string) (Session, bool) {
	if token == "" {
		return Session{}, false
	}

	m.mu.RLock()
	session, ok := m.sessions[token]
	m.mu.RUnlock()
	if !ok {
		return m.getPersisted(token)
	}
	if !session.ExpiresAt.After(m.now()) {
		m.Delete(token)
		return Session{}, false
	}
	return session, true
}

func (m *SessionManager) getPersisted(token string) (Session, bool) {
	if m.store == nil {
		return Session{}, false
	}
	session, ok, err := m.store.Get(token)
	if err != nil {
		panic(fmt.Errorf("load auth session: %w", err))
	}
	if !ok {
		return Session{}, false
	}
	if !session.ExpiresAt.After(m.now()) {
		m.Delete(token)
		return Session{}, false
	}
	return session, true
}

func (m *SessionManager) Delete(token string) {
	if token == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, token)
	if m.store != nil {
		if err := m.store.Delete(token); err != nil {
			panic(fmt.Errorf("delete auth session: %w", err))
		}
	}
}

func (m *SessionManager) CleanupExpired() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupExpiredLocked()
}

func (m *SessionManager) cleanupExpiredLocked() {
	now := m.now()
	for token, session := range m.sessions {
		if !session.ExpiresAt.After(now) {
			delete(m.sessions, token)
		}
	}
	if m.store != nil {
		if err := m.store.DeleteExpired(now); err != nil {
			panic(fmt.Errorf("delete expired auth sessions: %w", err))
		}
	}
}

func generateToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
