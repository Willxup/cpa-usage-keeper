package test

import (
	"context"
	"cpa-usage-keeper/internal/cpa"
	. "cpa-usage-keeper/internal/quota"
	"testing"
	"time"
)

func TestTrafficRunnerUsesRuntimeContextAndStopsInFlightRequest(t *testing.T) {
	db := openQuotaTestDatabase(t)
	service := NewServiceWithRegistry(db, NewProviderRegistry(nil), emptyPricingCatalogForTest())
	defer service.StopRefreshTasks()
	// Installing the runtime context cancels the construction-time lease.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service.SetRefreshContext(ctx)
	reader := &blockingTrafficReader{started: make(chan context.Context, 1)}
	runner := NewTrafficQuotaSyncRunner(service, reader)
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()
	select {
	case request := <-reader.started:
		if request.Err() != nil {
			t.Fatal("polling used the canceled construction context")
		}
	case <-time.After(time.Second):
		t.Fatal("traffic polling did not start")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("normal shutdown returned an error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("traffic request did not stop with the application")
	}
}

type blockingTrafficReader struct{ started chan context.Context }

func (r *blockingTrafficReader) FetchQuotaObservations(ctx context.Context) ([]cpa.QuotaObservation, error) {
	r.started <- ctx
	<-ctx.Done()
	return nil, ctx.Err()
}
