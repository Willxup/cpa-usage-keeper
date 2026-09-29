package test

import (
	"context"
	"net/http"
	"net/http/httptest"
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

func TestAppSkipsRankingWhenDisabled(t *testing.T) {
	cfg := databasePoolTestConfig(filepath.Join(t.TempDir(), "ranking-disabled.db"))
	cfg.RankingEnabled = false
	cfg.APIKeyViewerLocalRankingEnabled = true
	application, err := keeperapp.NewWithConfig(cfg)
	if err != nil {
		t.Fatalf("NewWithConfig returned error: %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })
	// 接口字段必须是真正的 nil，typed nil 会让 App.Run 启动空 runner。
	if application.Ranking != nil || application.LocalRanking != nil {
		t.Fatalf("expected no ranking runners, got ranking=%v local=%v", application.Ranking, application.LocalRanking)
	}

	for _, target := range []string{"/api/v1/ranking/status", "/api/v1/ranking/local/leaderboards"} {
		response := httptest.NewRecorder()
		application.Router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
		if response.Code != http.StatusNotFound {
			t.Fatalf("expected %s to be absent, got %d %s", target, response.Code, response.Body.String())
		}
	}
}
