package test

import (
	"context"
	"path/filepath"
	"testing"

	keeperapp "cpa-usage-keeper/internal/app"
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
	application := newInitializedApp(t, cfg)
	if application.Ranking == nil {
		t.Fatal("expected App to construct ranking runner")
	}
	if err := application.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	started := make(chan struct{})
	runner := &rankingRunnerStub{started: started}
	appWithStub, err := keeperapp.NewWithConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = appWithStub.Close() })
	appWithStub.Ranking = runner
	if err := appWithStub.Run(); err == nil {
		t.Fatal("expected invalid port error")
	}
	select {
	case <-started:
		t.Fatal("ranking runner started before the HTTP shell could listen")
	default:
	}
}
