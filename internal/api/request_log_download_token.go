package api

import (
	"crypto/rand"
	"encoding/base64"
	"sync"
	"time"
)

const requestLogDownloadTokenTTL = time.Minute

type requestLogDownloadTokenStore struct {
	mu     sync.Mutex
	now    func() time.Time
	tokens map[string]requestLogDownloadToken
}

type requestLogDownloadToken struct {
	eventID   int64
	expiresAt time.Time
}

func newRequestLogDownloadTokenStore() *requestLogDownloadTokenStore {
	return &requestLogDownloadTokenStore{
		now:    time.Now,
		tokens: map[string]requestLogDownloadToken{},
	}
}

func (s *requestLogDownloadTokenStore) issue(eventID int64) (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw[:])

	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.pruneLocked(now)
	s.tokens[token] = requestLogDownloadToken{eventID: eventID, expiresAt: now.Add(requestLogDownloadTokenTTL)}
	return token, nil
}

func (s *requestLogDownloadTokenStore) consume(token string, eventID int64) bool {
	if token == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	entry, ok := s.tokens[token]
	if !ok {
		return false
	}
	delete(s.tokens, token)
	if !now.Before(entry.expiresAt) || entry.eventID != eventID {
		return false
	}
	return true
}

func (s *requestLogDownloadTokenStore) pruneLocked(now time.Time) {
	for token, entry := range s.tokens {
		if !now.Before(entry.expiresAt) {
			delete(s.tokens, token)
		}
	}
}
