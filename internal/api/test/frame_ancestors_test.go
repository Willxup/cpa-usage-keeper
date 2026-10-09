package test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	. "cpa-usage-keeper/internal/api"
)

func TestFrameAncestorsCSPOnlyAppliesToHTML(t *testing.T) {
	router := NewRouter(fstest.MapFS{
		"index.html":    {Data: []byte(`<html><head></head><body></body></html>`)},
		"assets/app.js": {Data: []byte(`console.log("ok")`)},
	}, nil, nil, nil, AuthConfig{FrameAncestorOrigins: []string{"https://cpa.example.com"}}, nil, "")

	for path, want := range map[string]string{
		"/":              "frame-ancestors 'self' https://cpa.example.com",
		"/assets/app.js": "",
	} {
		t.Run(path, func(t *testing.T) {
			resp := httptest.NewRecorder()
			router.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, path, nil))
			if csp := resp.Header().Get("Content-Security-Policy"); csp != want {
				t.Fatalf("frame-ancestors CSP = %q, want %q", csp, want)
			}
		})
	}
}
