package test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"cpa-usage-keeper/internal/oidc"
)

func TestTransactionCapacityExpiryAndBrowserIsolation(t *testing.T) {
	now := time.Now()
	store := oidc.NewTransactionStore(oidc.TransactionOptions{TTL: time.Minute, Capacity: 3, BrowserCapacity: 2, Now: func() time.Time { return now }})
	bindings := make(map[string]string)
	first, binding, err := store.Create("issuer", "client", "/one", "source", bindings)
	if err != nil {
		t.Fatal(err)
	}
	bindings[first.State] = binding
	second, secondBinding, err := store.Create("issuer", "client", "/two", "source", bindings)
	if err != nil || first.State == second.State || binding == secondBinding || first.Nonce == second.Nonce || first.Verifier == second.Verifier {
		t.Fatal("transactions are not independent", err)
	}
	bindings[second.State] = secondBinding
	if _, _, err := store.Create("issuer", "client", "/", "source", bindings); !errors.Is(err, oidc.ErrCapacity) {
		t.Fatalf("browser limit = %v", err)
	}
	if _, _, err := store.Create("issuer", "client", "/", "source", nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Create("issuer", "client", "/", "source", nil); !errors.Is(err, oidc.ErrCapacity) {
		t.Fatalf("global limit = %v", err)
	}
	if _, err := store.Consume(first.State, "wrong"); !errors.Is(err, oidc.ErrInvalid) {
		t.Fatal(err)
	}
	if item, err := store.Consume(first.State, binding); err != nil || item.ReturnTo != "/one" {
		t.Fatalf("wrong browser consumed original: %+v %v", item, err)
	}
	now = now.Add(2 * time.Minute)
	if _, err := store.Consume(second.State, secondBinding); !errors.Is(err, oidc.ErrExpired) {
		t.Fatal(err)
	}
	if _, _, err := store.Create("issuer", "client", "/new", "source", bindings); err != nil {
		t.Fatalf("expired entries occupied capacity: %v", err)
	}
}

func TestTransactionConcurrentConsumeExactlyOnce(t *testing.T) {
	store := oidc.NewTransactionStore(oidc.TransactionOptions{})
	transaction, binding, err := store.Create("issuer", "client", "/", "source", nil)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	successes := 0
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := store.Consume(transaction.State, binding); err == nil {
				mu.Lock()
				successes++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if successes != 1 {
		t.Fatalf("successes = %d", successes)
	}
}
