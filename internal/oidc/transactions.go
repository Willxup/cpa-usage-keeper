package oidc

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"sync"
	"time"
)

var (
	ErrInvalid     = errors.New("OIDC login failed")
	ErrExpired     = errors.New("OIDC login expired")
	ErrUnavailable = errors.New("OIDC identity service unavailable")
	ErrDenied      = errors.New("OIDC access denied")
	ErrCanceled    = errors.New("OIDC login canceled")
	ErrCapacity    = errors.New("too many OIDC login attempts")
)

const (
	TransactionTTL         = 5 * time.Minute
	MaxTransactions        = 1024
	MaxBrowserTransactions = 8
	BindingCookiePrefix    = "cpa_usage_keeper_oidc_"
)

type Transaction struct {
	State, Nonce, Verifier string
	Issuer, ClientID       string
	ReturnTo               string
	SourceKey              string
	ExpiresAt              time.Time
	bindingHash            [sha256.Size]byte
}

type TransactionOptions struct {
	TTL                       time.Duration
	Capacity, BrowserCapacity int
	Now                       func() time.Time
}

type TransactionStore struct {
	mu      sync.Mutex
	items   map[string]Transaction
	options TransactionOptions
}

func NewTransactionStore(options TransactionOptions) *TransactionStore {
	if options.TTL <= 0 {
		options.TTL = TransactionTTL
	}
	if options.Capacity <= 0 {
		options.Capacity = MaxTransactions
	}
	if options.BrowserCapacity <= 0 {
		options.BrowserCapacity = MaxBrowserTransactions
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return &TransactionStore{items: make(map[string]Transaction), options: options}
}

// Create 核对浏览器仍持有的独立绑定后计数，容量不足时不驱逐其他人的事务。
func (s *TransactionStore) Create(issuer, clientID, returnTo, sourceKey string, bindings map[string]string) (Transaction, string, error) {
	state, err := randomSecret()
	if err != nil {
		return Transaction{}, "", ErrUnavailable
	}
	nonce, err := randomSecret()
	if err != nil {
		return Transaction{}, "", ErrUnavailable
	}
	verifier, err := randomSecret()
	if err != nil {
		return Transaction{}, "", ErrUnavailable
	}
	binding, err := randomSecret()
	if err != nil {
		return Transaction{}, "", ErrUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.options.Now()
	active := 0
	for key, item := range s.items {
		if !item.ExpiresAt.After(now) {
			delete(s.items, key)
			continue
		}
		if secret, ok := bindings[key]; ok && matchesBinding(item, secret) {
			active++
		}
	}
	if len(s.items) >= s.options.Capacity || active >= s.options.BrowserCapacity {
		return Transaction{}, "", ErrCapacity
	}
	item := Transaction{State: state, Nonce: nonce, Verifier: verifier, Issuer: issuer, ClientID: clientID, ReturnTo: returnTo, SourceKey: sourceKey, ExpiresAt: now.Add(s.options.TTL), bindingHash: sha256.Sum256([]byte(binding))}
	s.items[state] = item
	return item, binding, nil
}

// Consume 只有匹配浏览器绑定才能原子取出；错误浏览器不能消耗原浏览器的机会。
func (s *TransactionStore) Consume(state, binding string) (Transaction, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[state]
	if !ok {
		return Transaction{}, ErrExpired
	}
	if !item.ExpiresAt.After(s.options.Now()) {
		delete(s.items, state)
		return Transaction{}, ErrExpired
	}
	if !matchesBinding(item, binding) {
		return Transaction{}, ErrInvalid
	}
	delete(s.items, state)
	return item, nil
}

func matchesBinding(item Transaction, binding string) bool {
	if binding == "" {
		return false
	}
	hash := sha256.Sum256([]byte(binding))
	return subtle.ConstantTimeCompare(hash[:], item.bindingHash[:]) == 1
}

func randomSecret() (string, error) {
	var value [32]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value[:]), nil
}
