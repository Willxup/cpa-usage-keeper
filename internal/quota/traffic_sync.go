package quota

import (
	"context"
	"cpa-usage-keeper/internal/cpa"
	"cpa-usage-keeper/internal/entities"
	"fmt"
	"math"
	"strings"
	"time"
)

type trafficQuotaReader interface {
	FetchQuotaObservations(context.Context) ([]cpa.QuotaObservation, error)
}

// StartTrafficQuotaSync reads CPA's local cache, never a provider endpoint.
// The existing worker revalidates identities and applies cache/history protections.
func (s *Service) StartTrafficQuotaSync(reader trafficQuotaReader) {
	if s == nil || reader == nil {
		return
	}
	s.trafficSyncOnce.Do(func() {
		s.startRefreshGoroutine(func() {
			timer := time.NewTicker(5 * time.Second)
			defer timer.Stop()
			seen := map[string]time.Time{}
			for {
				ctx, cancel := context.WithTimeout(s.refreshContextSnapshot(), 4*time.Second)
				seen, _ = s.syncTrafficQuota(ctx, reader, seen)
				cancel()
				select {
				case <-s.refreshContextSnapshot().Done():
					return
				case <-timer.C:
				}
			}
		})
	})
}

func (s *Service) syncTrafficQuota(ctx context.Context, reader trafficQuotaReader, seen map[string]time.Time) (map[string]time.Time, error) {
	items, err := reader.FetchQuotaObservations(ctx)
	if err != nil {
		return seen, err
	}
	if len(items) > 16384 {
		return seen, fmt.Errorf("too many quota observations")
	}
	candidates := make([]*UsageHeaderSnapshot, 0, len(items))
	validItems := make([]cpa.QuotaObservation, 0, len(items))
	now := time.Now()
	for _, item := range items {
		if item.ObservedAt.IsZero() || item.ObservedAt.After(now.Add(time.Minute)) {
			continue
		}
		snapshot, ok := BuildUsageHeaderSnapshot(UsageHeaderSnapshotInput{AuthType: "oauth", AuthIndex: item.AuthIndex, Provider: item.Provider, ObservedAt: item.ObservedAt, Headers: item.Headers, Source: trafficQuotaSource(item.Source)})
		if ok {
			candidates = append(candidates, snapshot)
			validItems = append(validItems, item)
		}
	}
	identities, err := s.usageHeaderIdentityLookup(ctx, candidates)
	if err != nil {
		return seen, err
	}
	snapshots, next := selectTrafficQuotaSnapshots(validItems, candidates, identities, seen)
	if !s.TryAppendUsageHeaderSnapshots(snapshots) {
		return seen, context.Canceled
	}
	return next, nil
}

func selectTrafficQuotaSnapshots(items []cpa.QuotaObservation, candidates []*UsageHeaderSnapshot, identities map[string]entities.UsageIdentity, seen map[string]time.Time) ([]*UsageHeaderSnapshot, map[string]time.Time) {
	snapshots := make([]*UsageHeaderSnapshot, 0, len(candidates))
	next := map[string]time.Time{}
	for i, snapshot := range candidates {
		identity, ok := identities[snapshot.AuthIndex]
		if !ok || identity.IsDeleted || (identity.Disabled != nil && *identity.Disabled) || identity.AuthType != entities.UsageIdentityAuthTypeAuthFile || !usageHeaderIdentityMatchesSnapshot(identity, snapshot) {
			continue
		}
		key := snapshot.AuthIndex + "\x00" + snapshot.Provider + "\x00" + items[i].Window
		previous := seen[key]
		if value := next[key]; value.After(previous) {
			previous = value
		}
		next[key] = previous
		if snapshot.ObservedAt.After(previous) {
			snapshots = append(snapshots, snapshot)
			next[key] = snapshot.ObservedAt
		}
	}
	return snapshots, next
}

func trafficQuotaSource(source string) RefreshSource {
	switch strings.TrimSpace(source) {
	case "websocket_event":
		return "websocket_event"
	case "scheduled_provider_query":
		return "scheduled_provider_query"
	case "manual_provider_query":
		return "manual_provider_query"
	default:
		return "api_response_headers"
	}
}

func stampQuotaRows(rows []QuotaRow, at time.Time, source RefreshSource) {
	if source == "" || source == RefreshSourceUsageHeader {
		source = "api_response_headers"
	}
	for i := range rows {
		rows[i].CapturedAt = at
		rows[i].Source = source
		if rows[i].ResetAt == "" && rows[i].ResetAfterSeconds != nil && *rows[i].ResetAfterSeconds >= 0 && *rows[i].ResetAfterSeconds <= math.MaxInt64/int64(time.Second) {
			rows[i].ResetAt = at.Add(time.Duration(*rows[i].ResetAfterSeconds) * time.Second).Format(time.RFC3339)
		}
	}
}
