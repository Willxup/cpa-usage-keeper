package test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"cpa-usage-keeper/internal/config"
)

const publicLoginPasswordPlaceholder = "replace-with-your-login-password"

func TestAuthenticationDefaultsToEnabledAndRequiresPassword(t *testing.T) {
	t.Setenv("AUTH_ENABLED", "")
	t.Setenv("LOGIN_PASSWORD", "")
	t.Setenv("TZ", "UTC")

	_, err := config.Load(config.LoadOptions{EnvFile: writeAuthConfig(t, "")})
	if err == nil || !strings.Contains(err.Error(), "AUTH_ENABLED is not set") || !strings.Contains(err.Error(), "LOGIN_PASSWORD is required") {
		t.Fatalf("expected missing default authentication password error, got %v", err)
	}
}

func TestAuthenticationCanBeExplicitlyDisabled(t *testing.T) {
	t.Setenv("AUTH_ENABLED", "")
	t.Setenv("LOGIN_PASSWORD", "")
	t.Setenv("TZ", "UTC")

	cfg, err := config.Load(config.LoadOptions{EnvFile: writeAuthConfig(t, "AUTH_ENABLED=false\n")})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.AuthEnabled {
		t.Fatal("expected explicit AUTH_ENABLED=false to remain supported")
	}
}

func TestAuthenticationRejectsPublicExamplePassword(t *testing.T) {
	t.Setenv("AUTH_ENABLED", "")
	t.Setenv("LOGIN_PASSWORD", "")
	t.Setenv("TZ", "UTC")

	_, err := config.Load(config.LoadOptions{EnvFile: writeAuthConfig(t, "AUTH_ENABLED=true\nLOGIN_PASSWORD="+publicLoginPasswordPlaceholder+"\n")})
	if err == nil || !strings.Contains(err.Error(), "public example value") {
		t.Fatalf("expected public example password error, got %v", err)
	}
}

func TestAuthenticationUsesSecureDefaultWithPrivatePassword(t *testing.T) {
	t.Setenv("AUTH_ENABLED", "")
	t.Setenv("LOGIN_PASSWORD", "")
	t.Setenv("TZ", "UTC")

	cfg, err := config.Load(config.LoadOptions{EnvFile: writeAuthConfig(t, "LOGIN_PASSWORD=private-test-password\n")})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if !cfg.AuthEnabled {
		t.Fatal("expected authentication to default to enabled")
	}
}

func TestTrustedProxyCIDRsAcceptExplicitProxyNetworks(t *testing.T) {
	t.Setenv("AUTH_ENABLED", "")
	t.Setenv("LOGIN_PASSWORD", "")
	t.Setenv("TRUSTED_PROXY_CIDRS", "")
	t.Setenv("TZ", "UTC")

	cfg, err := config.Load(config.LoadOptions{EnvFile: writeAuthConfig(t, "LOGIN_PASSWORD=private-test-password\nTRUSTED_PROXY_CIDRS=172.18.0.0/16,2001:db8::/64\n")})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	want := []string{"172.18.0.0/16", "2001:db8::/64"}
	if !slices.Equal(cfg.TrustedProxyCIDRs, want) {
		t.Fatalf("expected trusted proxy CIDRs %v, got %v", want, cfg.TrustedProxyCIDRs)
	}
}

func TestTrustedProxyCIDRsRejectInvalidOrUniversalNetworks(t *testing.T) {
	for _, value := range []string{"not-a-cidr", "0.0.0.0/0", "::/0"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("AUTH_ENABLED", "")
			t.Setenv("LOGIN_PASSWORD", "")
			t.Setenv("TRUSTED_PROXY_CIDRS", "")
			t.Setenv("TZ", "UTC")

			_, err := config.Load(config.LoadOptions{EnvFile: writeAuthConfig(t, "LOGIN_PASSWORD=private-test-password\nTRUSTED_PROXY_CIDRS="+value+"\n")})
			if err == nil || !strings.Contains(err.Error(), "TRUSTED_PROXY_CIDRS") {
				t.Fatalf("expected trusted proxy CIDR validation error, got %v", err)
			}
		})
	}
}

func writeAuthConfig(t *testing.T, extra string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "keeper.env")
	content := "CPA_BASE_URL=http://127.0.0.1:8317\nCPA_MANAGEMENT_KEY=test-management-key\nWORK_DIR=./data\n" + extra
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	return path
}
