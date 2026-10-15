package test

import (
	"strings"
	"testing"

	"cpa-usage-keeper/internal/config"
)

const oidcEnvBase = "LOGIN_PASSWORD=private-test-password\n" +
	"AUTH_OIDC_ISSUER=http://127.0.0.1:9999\n" +
	"AUTH_OIDC_CLIENT_ID=smoke-client\n" +
	"AUTH_OIDC_CLIENT_SECRET=smoke-secret\n" +
	"AUTH_OIDC_REDIRECT_URL=http://127.0.0.1:8080/api/v1/auth/oidc/callback\n"

func TestOIDCLoginDisabledByDefault(t *testing.T) {
	cfg, err := config.Load(config.LoadOptions{EnvFile: writeAuthConfig(t, "LOGIN_PASSWORD=private-test-password\n")})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.AuthOIDCEnabled {
		t.Fatal("expected OIDC login to default to disabled")
	}
	if cfg.AuthOIDCAllowedUsers != nil {
		t.Fatalf("expected empty OIDC allowlist by default, got %v", cfg.AuthOIDCAllowedUsers)
	}
}

func TestOIDCLoginEnabledRequiresAllFields(t *testing.T) {
	_, err := config.Load(config.LoadOptions{EnvFile: writeAuthConfig(t,
		"LOGIN_PASSWORD=private-test-password\nAUTH_OIDC_ENABLED=true\n")})
	if err == nil || !strings.Contains(err.Error(), "required when AUTH_OIDC_ENABLED is true") {
		t.Fatalf("expected missing OIDC fields error, got %v", err)
	}
}

func TestOIDCLoginEnabledWithFullConfig(t *testing.T) {
	cfg, err := config.Load(config.LoadOptions{EnvFile: writeAuthConfig(t, "AUTH_OIDC_ENABLED=true\n"+oidcEnvBase)})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if !cfg.AuthOIDCEnabled {
		t.Fatal("expected OIDC login to be enabled")
	}
	if !cfg.AuthOIDCConfigured() {
		t.Fatal("expected OIDC config to be reported as configured")
	}
}

// 非 TLS 的非回环 issuer 会让 discovery/JWKS 走明文通道，必须在启动时拒绝。
func TestOIDCLoginRejectsNonTLSIssuer(t *testing.T) {
	_, err := config.Load(config.LoadOptions{EnvFile: writeAuthConfig(t,
		"LOGIN_PASSWORD=private-test-password\nAUTH_OIDC_ENABLED=true\n"+
			"AUTH_OIDC_ISSUER=http://sso.example.com/realms/keeper\n"+
			"AUTH_OIDC_CLIENT_ID=smoke-client\n"+
			"AUTH_OIDC_CLIENT_SECRET=smoke-secret\n"+
			"AUTH_OIDC_REDIRECT_URL=https://keeper.example.com/api/v1/auth/oidc/callback\n")})
	if err == nil || !strings.Contains(err.Error(), "AUTH_OIDC_ISSUER") {
		t.Fatalf("expected non-TLS issuer to be rejected, got %v", err)
	}
}

// 逃生门:显式开启后允许内网明文 http issuer,但仍然要求绝对 http(s) 地址。
func TestOIDCLoginAllowsInsecureIssuerWhenOptedIn(t *testing.T) {
	cfg, err := config.Load(config.LoadOptions{EnvFile: writeAuthConfig(t,
		"LOGIN_PASSWORD=private-test-password\nAUTH_OIDC_ENABLED=true\n"+
			"AUTH_OIDC_ALLOW_INSECURE_ISSUER=true\n"+
			"AUTH_OIDC_ISSUER=http://sso.example.com/realms/keeper\n"+
			"AUTH_OIDC_CLIENT_ID=smoke-client\n"+
			"AUTH_OIDC_CLIENT_SECRET=smoke-secret\n"+
			"AUTH_OIDC_REDIRECT_URL=https://keeper.example.com/api/v1/auth/oidc/callback\n")})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if !cfg.AuthOIDCAllowInsecureIssuer {
		t.Fatal("expected insecure issuer opt-in to be recorded")
	}
}

func TestOIDCLoginParsesAllowlist(t *testing.T) {
	cfg, err := config.Load(config.LoadOptions{EnvFile: writeAuthConfig(t,
		oidcEnvBase+"AUTH_OIDC_ALLOWED_USERS=alice@example.com, bob@example.com\n")})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	want := []string{"alice@example.com", "bob@example.com"}
	if len(cfg.AuthOIDCAllowedUsers) != len(want) {
		t.Fatalf("allowlist = %v, want %v", cfg.AuthOIDCAllowedUsers, want)
	}
	for index, entry := range want {
		if cfg.AuthOIDCAllowedUsers[index] != entry {
			t.Fatalf("allowlist[%d] = %q, want %q", index, cfg.AuthOIDCAllowedUsers[index], entry)
		}
	}
}

func TestOIDCRedirectURLRequiresHTTPSOrLoopback(t *testing.T) {
	invalidEnv := "LOGIN_PASSWORD=private-test-password\n" +
		"AUTH_OIDC_ENABLED=true\n" +
		"AUTH_OIDC_ISSUER=http://127.0.0.1:9999\n" +
		"AUTH_OIDC_CLIENT_ID=smoke-client\n" +
		"AUTH_OIDC_CLIENT_SECRET=smoke-secret\n" +
		"AUTH_OIDC_REDIRECT_URL=http://keeper.example.com/api/v1/auth/oidc/callback\n"
	_, err := config.Load(config.LoadOptions{EnvFile: writeAuthConfig(t, invalidEnv)})
	if err == nil || !strings.Contains(err.Error(), "must use https") {
		t.Fatalf("expected https redirect URL error, got %v", err)
	}
}
