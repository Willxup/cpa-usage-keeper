package test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"cpa-usage-keeper/internal/updatecheck"
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
			got, ok := updatecheck.CompareStableVersions(tt.left, tt.right)
			if ok != tt.wantOK {
				t.Fatalf("updatecheck.CompareStableVersions() ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && got != tt.want {
				t.Fatalf("updatecheck.CompareStableVersions() = %d, want %d", got, tt.want)
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
			if got := updatecheck.IsStableVersion(tt.version); got != tt.want {
				t.Fatalf("updatecheck.IsStableVersion() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCheckerSelectsLatestVersion(t *testing.T) {
	for _, tc := range []struct {
		name                string
		releaseStatus       int
		release, tags, want string
	}{
		{"latest release", http.StatusOK, `{"tag_name":"v1.2.4"}`, "", "v1.2.4"},
		{"fallback to tags", http.StatusNotFound, "", `[{"name":"v1.2.5"},{"name":"v1.2.3"},{"name":"test"}]`, "v1.2.5"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/repos/Willxup/cpa-usage-keeper/releases/latest":
					w.WriteHeader(tc.releaseStatus)
					_, _ = w.Write([]byte(tc.release))
				case "/repos/Willxup/cpa-usage-keeper/tags":
					if tc.releaseStatus == http.StatusOK {
						t.Error("tags requested despite an available latest release")
					}
					_, _ = w.Write([]byte(tc.tags))
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			checker := updatecheck.NewChecker("v1.2.3", updatecheck.WithBaseURL(server.URL), updatecheck.WithHTTPClient(server.Client()))
			result, err := checker.Check(context.Background())
			if err != nil {
				t.Fatalf("Check() error = %v", err)
			}
			if result.LatestVersion != tc.want || !result.CanCompare || !result.UpdateAvailable {
				t.Fatalf("Check() = %+v, want comparable update %s", result, tc.want)
			}
		})
	}
}

func TestCheckerDoesNotCompareDevVersion(t *testing.T) {
	var called atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called.Store(true)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	checker := updatecheck.NewChecker("dev", updatecheck.WithBaseURL(server.URL), updatecheck.WithHTTPClient(server.Client()))
	result, err := checker.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if called.Load() {
		t.Fatalf("Check() called GitHub for dev version")
	}
	if result.CanCompare {
		t.Fatalf("CanCompare = true, want false")
	}
	if result.UpdateAvailable {
		t.Fatalf("UpdateAvailable = true, want false")
	}
}
