package test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/cpa"
)

func TestLoadAppHostPrecedence(t *testing.T) {
	t.Run("default preserves all-interface listen address", func(t *testing.T) {
		isolateConfigEnv(t)
		setRequiredConfig(t)

		cfg, err := config.LoadFromEnv()
		if err != nil {
			t.Fatalf("LoadFromEnv returned error: %v", err)
		}
		if cfg.AppHost != "" {
			t.Fatalf("expected empty default app host, got %q", cfg.AppHost)
		}
		if got := cfg.ListenAddress(); got != ":8080" {
			t.Fatalf("expected existing listen address :8080, got %q", got)
		}
	})

	t.Run("env file configures listen host", func(t *testing.T) {
		isolateConfigEnv(t)

		cfg, err := config.Load(config.LoadOptions{EnvFile: writeConfigFile(t, "127.0.0.1")})
		if err != nil {
			t.Fatalf("Load returned error: %v", err)
		}
		if cfg.AppHost != "127.0.0.1" {
			t.Fatalf("expected trimmed environment host, got %q", cfg.AppHost)
		}
		if got := cfg.ListenAddress(); got != "127.0.0.1:8080" {
			t.Fatalf("expected loopback listen address, got %q", got)
		}
	})

	t.Run("startup option overrides env file", func(t *testing.T) {
		isolateConfigEnv(t)

		cfg, err := config.Load(config.LoadOptions{
			EnvFile: writeConfigFile(t, "127.0.0.1"),
			AppHost: " ::1 ",
		})
		if err != nil {
			t.Fatalf("Load returned error: %v", err)
		}
		if cfg.AppHost != "::1" {
			t.Fatalf("expected startup host override, got %q", cfg.AppHost)
		}
		if got := cfg.ListenAddress(); got != "[::1]:8080" {
			t.Fatalf("expected IPv6 listen address, got %q", got)
		}
	})
}

func setRequiredConfig(t *testing.T) {
	t.Helper()
	t.Setenv("CPA_BASE_URL", "http://127.0.0.1:"+cpa.ManagementRedisDefaultPort)
	t.Setenv("CPA_MANAGEMENT_KEY", "secret")
}

func writeConfigFile(t *testing.T, appHost string) string {
	t.Helper()
	envPath := filepath.Join(t.TempDir(), "keeper.env")
	content := fmt.Sprintf(
		"CPA_BASE_URL=http://127.0.0.1:%s\nCPA_MANAGEMENT_KEY=secret\nAPP_HOST=%s\n",
		cpa.ManagementRedisDefaultPort,
		appHost,
	)
	if err := os.WriteFile(envPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	return envPath
}
