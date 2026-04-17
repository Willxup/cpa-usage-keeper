package auth

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

type SessionManager struct {
	ttl      time.Duration
	now      func() time.Time
	generate func() (string, error)

	mu       sync.RWMutex
	sessions map[string]time.Time
}

func NewSessionManager(ttl time.Duration) *SessionManager {
	return &SessionManager{
		ttl:      ttl,
		now:      time.Now,
		generate: generateToken,
		sessions: make(map[string]time.Time),
	}
}

func (m *SessionManager) Create() (string, time.Time, error) {
	token, err := m.generate()
	if err != nil {
		return "", time.Time{}, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.cleanupExpiredLocked()
	expiresAt := m.now().Add(m.ttl)
	m.sessions[token] = expiresAt

	return token, expiresAt, nil
}

func (m *SessionManager) Validate(token string) bool {
	if token == "" {
		return false
	}

	m.mu.RLock()
	expiresAt, ok := m.sessions[token]
	m.mu.RUnlock()
	if !ok {
		return false
	}
	if !expiresAt.After(m.now()) {
		m.Delete(token)
		return false
	}
	return true
}

func (m *SessionManager) Delete(token string) {
	if token == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, token)
}

func (m *SessionManager) CleanupExpired() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupExpiredLocked()
}

func (m *SessionManager) cleanupExpiredLocked() {
	now := m.now()
	for token, expiresAt := range m.sessions {
		if !expiresAt.After(now) {
			delete(m.sessions, token)
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
