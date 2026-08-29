package test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	keeperapp "cpa-usage-keeper/internal/app"
	"cpa-usage-keeper/internal/config"
	"github.com/gin-gonic/gin"
)

type rankingRunnerStub struct {
	started chan struct{}
}

func (s *rankingRunnerStub) Run(ctx context.Context) error {
	close(s.started)
	<-ctx.Done()
	return nil
}

func TestAppConstructsAndStartsRankingRunner(t *testing.T) {
	cfg := config.Config{
		AppPort: "invalid-port", CPABaseURL: "https://cpa.example.com", CPAManagementKey: "secret",
		RedisQueueIdleInterval: time.Second, MetadataSyncInterval: 30 * time.Second,
		SQLitePath: filepath.Join(t.TempDir(), "ranking-wiring.db"), RequestTimeout: 5 * time.Second,
		LogLevel: "info", LogFileEnabled: false, LogRetentionDays: 7,
	}
	application, err := keeperapp.NewWithConfig(cfg)
	if err != nil {
		t.Fatalf("NewWithConfig returned error: %v", err)
	}
	if application.Ranking == nil {
		_ = application.Close()
		t.Fatal("expected App to construct ranking runner")
	}
	if err := application.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	started := make(chan struct{})
	runner := &rankingRunnerStub{started: started}
	appWithStub := &keeperapp.App{Config: &cfg, Router: gin.New(), Ranking: runner}
	if err := appWithStub.Run(); err == nil {
		t.Fatal("expected invalid port error")
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("expected App.Run to start ranking runner")
	}
}
