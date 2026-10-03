package test

import (
	"context"
	. "cpa-usage-keeper/internal/app"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestConstructionDefersTrafficPollingUntilRun(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v8/management/quota/observations" {
			calls.Add(1)
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer upstream.Close()
	cfg := testAppConfig(t)
	cfg.CPABaseURL = upstream.URL
	application, err := NewWithConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer application.Close()
	if application.QuotaTrafficSync == nil {
		t.Fatal("traffic quota runner was not wired")
	}
	// Construction must not start network polling on a context that Run replaces.
	time.Sleep(50 * time.Millisecond)
	if calls.Load() != 0 {
		t.Fatal("quota polling started before the application lifecycle")
	}
}

func TestRunStartsTrafficPollingAfterContextAndWaitsForCancellation(t *testing.T) {
	cfg := testAppConfig(t)
	cfg.AppPort = "invalid-port"
	contexts := make(chan context.Context, 1)
	finished := make(chan bool, 1)
	application := &App{
		Config:           &cfg,
		Router:           gin.New(),
		QuotaService:     &quotaContextRecorder{contextSet: contexts},
		QuotaTrafficSync: &trafficLifecycleRecorder{contexts: contexts, finished: finished},
	}
	if err := application.Run(); err == nil {
		t.Fatal("expected invalid HTTP port error")
	}
	select {
	case same := <-finished:
		if !same {
			t.Fatal("traffic runner did not receive the installed quota context")
		}
	default:
		t.Fatal("Run returned before traffic polling stopped")
	}
}

type trafficLifecycleRecorder struct {
	contexts <-chan context.Context
	finished chan<- bool
}

func (r *trafficLifecycleRecorder) Run(ctx context.Context) error {
	select {
	case installed := <-r.contexts:
		<-ctx.Done()
		r.finished <- installed == ctx
	case <-time.After(time.Second):
		r.finished <- false
	}
	return nil
}
