package test

import (
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
)

func TestLoadFromEnvTimeZone(t *testing.T) {
	for _, tc := range []struct{ value, want string }{
		{"", "Asia/Shanghai"},
		{"UTC", "UTC"},
		{"America/New_York", "America/New_York"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			isolateConfigEnv(t)
			setRequiredConfig(t)
			t.Setenv("TZ", tc.value)
			if _, err := config.LoadFromEnv(); err != nil {
				t.Fatalf("LoadFromEnv: %v", err)
			}
			if got := time.Local.String(); got != tc.want {
				t.Fatalf("time.Local = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLoadFromEnvRejectsInvalidTimeZone(t *testing.T) {
	isolateConfigEnv(t)
	setRequiredConfig(t)
	t.Setenv("TZ", "Not/AZone")
	_, err := config.LoadFromEnv()
	if err == nil || !strings.Contains(err.Error(), "TZ is invalid") {
		t.Fatalf("expected invalid TZ error, got %v", err)
	}
}
