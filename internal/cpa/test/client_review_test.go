package cpa_test

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"cpa-usage-keeper/internal/cpa"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestNewClientHandlesCustomDefaultTransport(t *testing.T) {
	previousTransport := http.DefaultTransport
	http.DefaultTransport = roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("unexpected custom default transport use")
	})
	t.Cleanup(func() {
		http.DefaultTransport = previousTransport
	})

	if client := cpa.NewClient("https://example.test", "management-secret", time.Second, false); client == nil {
		t.Fatalf("expected client to be initialized")
	}
}
