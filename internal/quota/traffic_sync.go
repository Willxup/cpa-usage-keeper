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

// TrafficQuotaSyncRunner reads CPA's local cache, never a provider endpoint.
// App starts it after installing the quota service's runtime context.
type TrafficQuotaSyncRunner struct {
	service *Service
	reader  trafficQuotaReader
}

func NewTrafficQuotaSyncRunner(service *Service, reader trafficQuotaReader) *TrafficQuotaSyncRunner {
	return &TrafficQuotaSyncRunner{service: service, reader: reader}
}

func (r *TrafficQuotaSyncRunner) Run(ctx context.Context) error {
	if r == nil || r.service == nil || r.reader == nil {
		return nil
	}
	timer := time.NewTicker(5 * time.Second)
	defer timer.Stop()
	seen := map[string]time.Time{}
	for {
		if ctx.Err() != nil {
			return nil
		}
		requestCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
		seen, _ = r.service.syncTrafficQuota(requestCtx, r.reader, seen)
		cancel()
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
		}
	}
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
	// API providers retain a cache-only projection. They never enter OAuth
	// history or the Auth File worker, even when they use Codex/Claude headers.
	apiIdentities := map[string]entities.UsageIdentity{}
	if indexes := usageHeaderSnapshotAuthIndexes(candidates); len(indexes) > 0 {
		var rows []entities.UsageIdentity
		if err := s.db.WithContext(ctx).Where("identity IN ? AND auth_type = ? AND is_deleted = ? AND (disabled IS NULL OR disabled = ?)", indexes, entities.UsageIdentityAuthTypeAIProvider, false, false).Find(&rows).Error; err != nil {
			return seen, err
		}
		for _, row := range rows {
			apiIdentities[row.Identity] = row
		}
	}
	snapshots, next := selectTrafficQuotaSnapshots(validItems, candidates, identities, seen)
	for i, snapshot := range candidates {
		identity, ok := apiIdentities[snapshot.AuthIndex]
		if !ok || !usageHeaderIdentityMatchesSnapshot(identity, snapshot) {
			continue
		}
		key := snapshot.AuthIndex + "\x00" + snapshot.Provider + "\x00" + validItems[i].Window
		previous := seen[key]
		if next[key].After(previous) {
			previous = next[key]
		}
		next[key] = previous
		if snapshot.ObservedAt.After(previous) && s.applyTrafficAPIProviderQuota(ctx, snapshot, identity) {
			next[key] = snapshot.ObservedAt
		}
	}
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

// applyTrafficAPIProviderQuota is deliberately cache-only: API provider
// credentials must not be represented as OAuth accounts in quota history.
func (s *Service) applyTrafficAPIProviderQuota(ctx context.Context, snapshot *UsageHeaderSnapshot, identity entities.UsageIdentity) bool {
	if snapshot == nil || identity.AuthType != entities.UsageIdentityAuthTypeAIProvider || identity.IsDeleted || (identity.Disabled != nil && *identity.Disabled) || !usageHeaderIdentityMatchesSnapshot(identity, snapshot) {
		return false
	}
	response := CheckResponse{ID: snapshot.AuthIndex, Quota: NormalizeQuotaRows(snapshot.CacheOutput)}
	stampQuotaRows(response.Quota, snapshot.ObservedAt, snapshot.Source)
	response = s.attachWindowUsageStats(ctx, snapshot.AuthIndex, response, snapshot.ObservedAt)
	var current entities.UsageIdentity
	if err := s.db.WithContext(ctx).First(&current, identity.ID).Error; err != nil {
		return false
	}
	if current.AuthType != entities.UsageIdentityAuthTypeAIProvider || current.Identity != snapshot.AuthIndex || current.IsDeleted || (current.Disabled != nil && *current.Disabled) || !usageHeaderIdentityMatchesSnapshot(current, snapshot) {
		return false
	}
	return s.mergeUsageHeaderQuotaCache(snapshot.AuthIndex, response, snapshot.ObservedAt, current)
}
