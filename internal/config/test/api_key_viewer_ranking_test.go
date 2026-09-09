package test

import (
	"testing"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/cpa"
)

func TestAPIKeyViewerLocalRankingAccessDefaultsDisabled(t *testing.T) {
	isolateConfigEnv(t)
	t.Setenv("CPA_BASE_URL", "http://127.0.0.1:"+cpa.ManagementRedisDefaultPort)
	t.Setenv("CPA_MANAGEMENT_KEY", "secret")

	cfg, err := config.LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv returned error: %v", err)
	}
	if cfg.APIKeyViewerLocalRankingEnabled {
		t.Fatal("expected API Key Viewer local ranking access to be disabled by default")
	}
}

func TestAPIKeyViewerLocalRankingAccessReadsExplicitFlag(t *testing.T) {
	isolateConfigEnv(t)
	t.Setenv("CPA_BASE_URL", "http://127.0.0.1:"+cpa.ManagementRedisDefaultPort)
	t.Setenv("CPA_MANAGEMENT_KEY", "secret")
	t.Setenv("API_KEY_VIEWER_LOCAL_RANKING_ENABLED", "true")

	cfg, err := config.LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv returned error: %v", err)
	}
	if !cfg.APIKeyViewerLocalRankingEnabled {
		t.Fatal("expected explicit API Key Viewer local ranking access to be enabled")
	}
}
