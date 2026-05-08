package updatecheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCompareStableVersions(t *testing.T) {
	tests := []struct {
		name   string
		left   string
		right  string
		want   int
		wantOK bool
	}{
		{name: "patch version increases", left: "v1.2.3", right: "v1.2.4", want: -1, wantOK: true},
		{name: "minor version handles two digits", left: "v1.10.0", right: "v1.2.9", want: 1, wantOK: true},
		{name: "major version handles two digits", left: "v12.3.45", right: "v2.99.99", want: 1, wantOK: true},
		{name: "same version", left: "v1.2.3", right: "v1.2.3", want: 0, wantOK: true},
		{name: "dev is not comparable", left: "dev", right: "v1.2.3", wantOK: false},
		{name: "missing v prefix is not comparable", left: "1.2.3", right: "v1.2.3", wantOK: false},
		{name: "prerelease is not comparable", left: "v1.2.3-beta", right: "v1.2.3", wantOK: false},
		{name: "short version is not comparable", left: "v1.2", right: "v1.2.3", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := CompareStableVersions(tt.left, tt.right)
			if ok != tt.wantOK {
				t.Fatalf("CompareStableVersions() ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if got != tt.want {
				t.Fatalf("CompareStableVersions() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestIsStableVersion(t *testing.T) {
	tests := []struct {
		version string
		want    bool
	}{
		{version: "v1.2.3", want: true},
		{version: "v12.3.45", want: true},
		{version: "dev", want: false},
		{version: "1.2.3", want: false},
		{version: "v1.2", want: false},
		{version: "v1.2.3-beta", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			if got := IsStableVersion(tt.version); got != tt.want {
				t.Fatalf("IsStableVersion() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCheckerUsesLatestRelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/Willxup/cpa-usage-keeper/releases/latest" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v1.2.4"}`))
	}))
	defer server.Close()

	checker := NewChecker("v1.2.3", WithBaseURL(server.URL), WithHTTPClient(server.Client()))
	result, err := checker.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	if !result.CanCompare {
		t.Fatalf("CanCompare = false, want true")
	}
	if !result.UpdateAvailable {
		t.Fatalf("UpdateAvailable = false, want true")
	}
	if result.LatestVersion != "v1.2.4" {
		t.Fatalf("LatestVersion = %q, want v1.2.4", result.LatestVersion)
	}
}

func TestCheckerFallsBackToTags(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/Willxup/cpa-usage-keeper/releases/latest":
			http.NotFound(w, r)
		case "/repos/Willxup/cpa-usage-keeper/tags":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"name":"v1.2.5"},{"name":"v1.2.3"},{"name":"test"}]`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	checker := NewChecker("v1.2.3", WithBaseURL(server.URL), WithHTTPClient(server.Client()))
	result, err := checker.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	if result.LatestVersion != "v1.2.5" {
		t.Fatalf("LatestVersion = %q, want v1.2.5", result.LatestVersion)
	}
	if !result.UpdateAvailable {
		t.Fatalf("UpdateAvailable = false, want true")
	}
}

func TestCheckerDoesNotCompareDevVersion(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	checker := NewChecker("dev", WithBaseURL(server.URL), WithHTTPClient(server.Client()))
	result, err := checker.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if called {
		t.Fatalf("Check() called GitHub for dev version")
	}
	if result.CanCompare {
		t.Fatalf("CanCompare = true, want false")
	}
	if result.UpdateAvailable {
		t.Fatalf("UpdateAvailable = true, want false")
	}
}
