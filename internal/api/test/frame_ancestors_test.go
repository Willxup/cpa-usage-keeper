package test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	. "cpa-usage-keeper/internal/api"
)

func TestHTMLResponseIncludesFrameAncestorsCSP(t *testing.T) {
	router := NewRouter(
		fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte(`<html><head></head><body></body></html>`)}},
		nil,
		nil,
		nil,
		AuthConfig{FrameAncestorOrigins: []string{"https://cpa.example.com"}},
		nil,
		"",
	)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	router.ServeHTTP(resp, req)

	csp := resp.Header().Get("Content-Security-Policy")
	if csp != "frame-ancestors 'self' https://cpa.example.com" {
		t.Fatalf("expected exact frame-ancestors CSP, got %q", csp)
	}
}

func TestStaticAssetDoesNotUseFrameAncestorsCSP(t *testing.T) {
	router := NewRouter(
		fstest.MapFS{
			"index.html":    &fstest.MapFile{Data: []byte(`<html><head></head><body></body></html>`)},
			"assets/app.js": &fstest.MapFile{Data: []byte(`console.log("ok")`)},
		},
		nil,
		nil,
		nil,
		AuthConfig{FrameAncestorOrigins: []string{"https://cpa.example.com"}},
		nil,
		"",
	)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)
	router.ServeHTTP(resp, req)

	if csp := resp.Header().Get("Content-Security-Policy"); csp != "" {
		t.Fatalf("expected static asset not to include frame-ancestors CSP, got %q", csp)
	}
}
