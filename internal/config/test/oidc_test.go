package test

import (
	"strings"
	"testing"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/oidc"
)

const validOIDCEnv = "AUTH_ENABLED=true\nLOGIN_PASSWORD=private-password\nAUTH_OIDC_ENABLED=true\nAUTH_OIDC_ISSUER=http://idp.example/realm/\nAUTH_OIDC_CLIENT_ID=keeper\nAUTH_OIDC_CLIENT_SECRET=private-secret\nAUTH_OIDC_REDIRECT_URL=https://keeper.example/cpa/api/v1/auth/oidc/callback\nAPP_BASE_PATH=/cpa\n"

func TestOIDCConfigDefaultsAndExplicitHTTPHTTPS(t *testing.T) {
	cfg, err := config.Load(config.LoadOptions{EnvFile: writeAuthConfig(t, "LOGIN_PASSWORD=private-password\n")})
	if err != nil || cfg.OIDC.Enabled || !cfg.OIDC.PromptLogin {
		t.Fatalf("OIDC default = %+v, %v", cfg, err)
	}
	for _, issuerScheme := range []string{"http", "https"} {
		for _, callbackScheme := range []string{"http", "https"} {
			body := strings.ReplaceAll(validOIDCEnv, "http://idp", issuerScheme+"://idp")
			body = strings.ReplaceAll(body, "https://keeper", callbackScheme+"://keeper")
			cfg, err := config.Load(config.LoadOptions{EnvFile: writeAuthConfig(t, body+"AUTH_OIDC_ALLOWED_SUBJECTS='[\"User,A\",\"user,a\",\" spaced \"]'\n")})
			if err != nil || !cfg.OIDC.Enabled || len(cfg.OIDC.AllowedSubjects) != 3 || cfg.OIDC.AllowedSubjects[2] != " spaced " {
				t.Fatalf("config = %+v, %v", cfg, err)
			}
		}
	}
}

func TestOIDCPromptLoginConfiguration(t *testing.T) {
	for _, value := range []string{"", "true", "false", "invalid"} {
		t.Run("value="+value, func(t *testing.T) {
			cfg, err := config.Load(config.LoadOptions{EnvFile: writeAuthConfig(t, validOIDCEnv+"AUTH_OIDC_PROMPT_LOGIN="+value+"\n")})
			if value == "invalid" {
				if err == nil || !strings.Contains(err.Error(), "AUTH_OIDC_PROMPT_LOGIN") {
					t.Fatalf("invalid setting error = %v", err)
				}
				return
			}
			if err != nil || cfg.OIDC.PromptLogin != (value != "false") {
				t.Fatalf("prompt login config = %+v, %v", cfg, err)
			}
		})
	}
}

func TestOIDCConfigRejectsInvalidCombinationsAndLegacyRestrictions(t *testing.T) {
	for name, body := range map[string]string{
		"disabled auth":           strings.ReplaceAll(validOIDCEnv, "AUTH_ENABLED=true", "AUTH_ENABLED=false"),
		"missing password":        strings.ReplaceAll(validOIDCEnv, "LOGIN_PASSWORD=private-password", "LOGIN_PASSWORD="),
		"missing client":          strings.ReplaceAll(validOIDCEnv, "AUTH_OIDC_CLIENT_ID=keeper", "AUTH_OIDC_CLIENT_ID="),
		"missing secret":          strings.ReplaceAll(validOIDCEnv, "AUTH_OIDC_CLIENT_SECRET=private-secret", "AUTH_OIDC_CLIENT_SECRET="),
		"issuer query":            strings.ReplaceAll(validOIDCEnv, "http://idp.example/realm/", "http://idp.example/realm/?tenant=1"),
		"issuer empty fragment":   strings.ReplaceAll(validOIDCEnv, "http://idp.example/realm/", "http://idp.example/realm/#"),
		"issuer credentials":      strings.ReplaceAll(validOIDCEnv, "http://idp.example/realm/", "https://user:pass@idp.example"),
		"callback base":           strings.ReplaceAll(validOIDCEnv, "keeper.example/cpa/", "keeper.example/other/"),
		"callback query":          strings.ReplaceAll(validOIDCEnv, "/oidc/callback", "/oidc/callback?extra=1"),
		"callback empty fragment": strings.ReplaceAll(validOIDCEnv, "/oidc/callback", "/oidc/callback#"),
		"legacy users":            validOIDCEnv + "AUTH_OIDC_ALLOWED_USERS=user@example.com\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := config.Load(config.LoadOptions{EnvFile: writeAuthConfig(t, body)})
			if err == nil {
				t.Fatal("invalid config accepted")
			}
			if name == "legacy users" && !strings.Contains(err.Error(), "AUTH_OIDC_ALLOWED_SUBJECTS") {
				t.Fatalf("missing migration guidance: %v", err)
			}
		})
	}
}

func TestOIDCSubjectArrayIsExactAndCannotBecomeAllowAllOnParseError(t *testing.T) {
	for _, value := range []string{`null`, `{}`, `"user"`, `[1]`, `[null]`, `[""]`, `["same","same"]`, `["valid",]`} {
		if _, err := oidc.ParseAllowedSubjects(value); err == nil {
			t.Fatalf("invalid subjects accepted: %s", value)
		}
	}
	for _, value := range []string{"", "[]"} {
		if subjects, err := oidc.ParseAllowedSubjects(value); err != nil || len(subjects) != 0 {
			t.Fatalf("empty policy = %v %v", subjects, err)
		}
	}
	subjects, err := oidc.ParseAllowedSubjects(`["Case","case","a,b"," spaced "]`)
	if err != nil {
		t.Fatal(err)
	}
	policy := oidc.Config{AllowedSubjects: subjects}
	if !policy.AllowsSubject("Case") || !policy.AllowsSubject("a,b") || !policy.AllowsSubject(" spaced ") || policy.AllowsSubject("CASE") || policy.AllowsSubject("spaced") {
		t.Fatal("subject policy changed exact identity")
	}
}
