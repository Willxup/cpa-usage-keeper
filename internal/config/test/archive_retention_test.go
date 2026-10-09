package test

import (
	"cpa-usage-keeper/internal/config"
	"testing"
)

func TestRawRetentionConfig(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  int
	}{{"", 0}, {"0", 0}, {"-1", 0}, {"30", 0}, {"89", 0}, {"90", 90}, {"180", 180}} {
		t.Run(tc.value, func(t *testing.T) {
			withIsolatedEnvFiles(t)
			t.Setenv("CPA_BASE_URL", "http://localhost:8317")
			t.Setenv("CPA_MANAGEMENT_KEY", "secret")
			t.Setenv("USAGE_RAW_RETENTION_DAYS", tc.value)
			cfg, err := config.LoadFromEnv()
			if err != nil {
				t.Fatal(err)
			}
			if cfg.UsageRawRetentionDays != tc.want {
				t.Fatalf("got %d want %d", cfg.UsageRawRetentionDays, tc.want)
			}
		})
	}
}
