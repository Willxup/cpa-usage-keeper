package test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	keeperapp "cpa-usage-keeper/internal/app"
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
	cfg := databasePoolTestConfig(filepath.Join(t.TempDir(), "ranking-wiring.db"))
	application, err := keeperapp.NewWithConfig(cfg)
	if err != nil {
		t.Fatalf("NewWithConfig returned error: %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })
	if application.Ranking == nil {
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
