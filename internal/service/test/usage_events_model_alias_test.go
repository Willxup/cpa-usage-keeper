package test

import (
	"context"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
)

func TestUsageServicePreservesEventMetadataForListAndStream(t *testing.T) {
	db := openUsageServiceTestDatabase(t)
	modelAlias := " sonnet-business "
	responseModel := " gpt-5.6-luna "
	statusCode := 200
	stream := true
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey:            "model-alias-event",
		APIGroupKey:         "provider-a",
		Model:               "claude-sonnet",
		ModelAlias:          &modelAlias,
		ResponseModel:       responseModel,
		ServiceTier:         "auto",
		ResponseServiceTier: "default",
		StatusCode:          &statusCode,
		Stream:              &stream,
		Timestamp:           time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC),
		InputTokens:         10,
		TotalTokens:         10,
	}}); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	provider := service.NewUsageService(db, emptyPricingCatalogForTest())
	page, err := provider.ListUsageEvents(context.Background(), servicedto.UsageFilter{Page: 1, PageSize: 10, Limit: 10})
	if err != nil {
		t.Fatalf("ListUsageEvents returned error: %v", err)
	}

	var streamed []servicedto.UsageEventRecord
	if err := provider.StreamUsageEvents(context.Background(), servicedto.UsageFilter{}, func(event servicedto.UsageEventRecord) error {
		streamed = append(streamed, event)
		return nil
	}); err != nil {
		t.Fatalf("StreamUsageEvents returned error: %v", err)
	}
	for name, events := range map[string][]servicedto.UsageEventRecord{"list": page.Events, "stream": streamed} {
		if len(events) != 1 || events[0].ModelAlias != "sonnet-business" || events[0].ResponseModel != "gpt-5.6-luna" || events[0].ServiceTier != "auto" || events[0].ResponseServiceTier != "default" || events[0].StatusCode == nil || *events[0].StatusCode != statusCode || events[0].Stream == nil || !*events[0].Stream {
			t.Fatalf("%s did not preserve event metadata: %+v", name, events)
		}
	}
}
