package quota

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/timeutil"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

const (
	usageHeaderSnapshotFlushInterval = 30 * time.Second
)

func (s *Service) TryAppendUsageHeaderSnapshots(snapshots []UsageHeaderSnapshot) bool {
	if s == nil || len(snapshots) == 0 {
		return true
	}
	s.usageHeaderMu.Lock()
	defer s.usageHeaderMu.Unlock()
	if s.usageHeaderClosing {
		return false
	}
	if !s.acquireUsageHeaderSlot() {
		return false
	}
	cloned := cloneUsageHeaderSnapshots(snapshots)
	select {
	case s.usageHeaderCh <- cloned:
		return true
	default:
		s.releaseUsageHeaderSlot()
		return false
	}
}

func (s *Service) acquireUsageHeaderSlot() bool {
	if s == nil || s.usageHeaderSlots == nil {
		return false
	}
	select {
	case <-s.usageHeaderSlots:
		return true
	default:
		return false
	}
}

func (s *Service) releaseUsageHeaderSlot() {
	if s == nil || s.usageHeaderSlots == nil {
		return
	}
	select {
	case s.usageHeaderSlots <- struct{}{}:
	default:
	}
}

func (s *Service) runUsageHeaderSnapshotWorker() {
	defer close(s.usageHeaderDoneCh)
	flushInterval := s.usageHeaderFlushInterval
	if flushInterval <= 0 {
		flushInterval = usageHeaderSnapshotFlushInterval
	}
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()
	pending := make(map[string]UsageHeaderSnapshot)
	for {
		select {
		case snapshots := <-s.usageHeaderCh:
			s.releaseUsageHeaderSlot()
			mergePendingUsageHeaderSnapshots(pending, snapshots)
		case <-ticker.C:
			s.flushPendingUsageHeaderSnapshots(pending)
		case <-s.usageHeaderStopCh:
			s.drainUsageHeaderSnapshots(pending)
			s.flushPendingUsageHeaderSnapshots(pending)
			return
		}
	}
}

func (s *Service) drainUsageHeaderSnapshots(pending map[string]UsageHeaderSnapshot) {
	for {
		select {
		case snapshots := <-s.usageHeaderCh:
			s.releaseUsageHeaderSlot()
			mergePendingUsageHeaderSnapshots(pending, snapshots)
		default:
			return
		}
	}
}

func (s *Service) flushPendingUsageHeaderSnapshots(pending map[string]UsageHeaderSnapshot) {
	if len(pending) == 0 {
		return
	}
	snapshots := pendingUsageHeaderSnapshots(pending)
	clear(pending)
	s.applyUsageHeaderSnapshots(context.Background(), snapshots)
}

func mergePendingUsageHeaderSnapshots(pending map[string]UsageHeaderSnapshot, snapshots []UsageHeaderSnapshot) {
	for _, snapshot := range snapshots {
		authIndex := strings.TrimSpace(snapshot.AuthIndex)
		if authIndex == "" {
			authIndex = snapshot.Provider + "\x00" + snapshot.AuthType
		}
		if existing, ok := pending[authIndex]; !ok || usageHeaderSnapshotIsNewer(snapshot, existing) {
			pending[authIndex] = snapshot
		}
	}
}

func usageHeaderSnapshotIsNewer(candidate UsageHeaderSnapshot, existing UsageHeaderSnapshot) bool {
	if candidate.ObservedAt.IsZero() {
		return existing.ObservedAt.IsZero()
	}
	if existing.ObservedAt.IsZero() {
		return true
	}
	return !candidate.ObservedAt.Before(existing.ObservedAt)
}

func pendingUsageHeaderSnapshots(pending map[string]UsageHeaderSnapshot) []UsageHeaderSnapshot {
	keys := make([]string, 0, len(pending))
	for key := range pending {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	snapshots := make([]UsageHeaderSnapshot, 0, len(keys))
	for _, key := range keys {
		snapshots = append(snapshots, pending[key])
	}
	return snapshots
}

func (s *Service) stopUsageHeaderSnapshotWorker() {
	if s == nil || s.usageHeaderStopCh == nil || s.usageHeaderDoneCh == nil {
		return
	}
	s.usageHeaderCloseOnce.Do(func() {
		s.usageHeaderMu.Lock()
		s.usageHeaderClosing = true
		close(s.usageHeaderStopCh)
		s.usageHeaderMu.Unlock()
		<-s.usageHeaderDoneCh
	})
}

func (s *Service) applyUsageHeaderSnapshots(ctx context.Context, snapshots []UsageHeaderSnapshot) {
	if len(snapshots) == 0 {
		return
	}
	identityByAuthIndex, err := s.usageHeaderIdentityLookup(ctx, snapshots)
	if err != nil {
		logrus.WithError(err).WithField("snapshot_count", len(snapshots)).Warn("usage header quota identity lookup failed")
		return
	}
	statsProvider := s.usageHeaderWindowStatsProvider(ctx)
	for _, snapshot := range snapshots {
		authIndex := strings.TrimSpace(snapshot.AuthIndex)
		identity, ok := identityByAuthIndex[authIndex]
		if !ok {
			logUsageHeaderSnapshotIgnored(snapshot)
			continue
		}
		if !s.applyUsageHeaderSnapshotWithIdentity(ctx, snapshot, identity, statsProvider) {
			logUsageHeaderSnapshotIgnored(snapshot)
		}
	}
}

func logUsageHeaderSnapshotIgnored(snapshot UsageHeaderSnapshot) {
	logrus.WithFields(logrus.Fields{
		"auth_index": snapshot.AuthIndex,
		"provider":   snapshot.Provider,
	}).Debug("usage header quota snapshot ignored")
}

func (s *Service) usageHeaderIdentityLookup(ctx context.Context, snapshots []UsageHeaderSnapshot) (map[string]entities.UsageIdentity, error) {
	authIndexes := usageHeaderSnapshotAuthIndexes(snapshots)
	if len(authIndexes) == 0 {
		return map[string]entities.UsageIdentity{}, nil
	}
	identities, err := repository.ListActiveAuthFileUsageIdentitiesByAuthIndexes(ctx, s.db, authIndexes)
	if err != nil {
		return nil, err
	}
	identityByAuthIndex := make(map[string]entities.UsageIdentity, len(identities))
	for _, identity := range identities {
		authIndex := strings.TrimSpace(identity.Identity)
		if authIndex == "" || !usageHeaderIdentityIsCodex(identity) {
			continue
		}
		identityByAuthIndex[authIndex] = identity
	}
	return identityByAuthIndex, nil
}

func usageHeaderSnapshotAuthIndexes(snapshots []UsageHeaderSnapshot) []string {
	authIndexes := make([]string, 0, len(snapshots))
	seen := make(map[string]struct{}, len(snapshots))
	for _, snapshot := range snapshots {
		authType := strings.ToLower(strings.TrimSpace(snapshot.AuthType))
		authIndex := strings.TrimSpace(snapshot.AuthIndex)
		if authType != "oauth" || authIndex == "" {
			continue
		}
		if _, ok := seen[authIndex]; ok {
			continue
		}
		seen[authIndex] = struct{}{}
		authIndexes = append(authIndexes, authIndex)
	}
	return authIndexes
}

func (s *Service) usageHeaderWindowStatsProvider(ctx context.Context) usageWindowStatsProvider {
	calculator, err := repository.NewUsageWindowStatsCalculator(ctx, s.db)
	if err != nil {
		logrus.WithError(err).Debug("usage header quota window stats calculator unavailable")
		return nil
	}
	return calculator
}

func (s *Service) applyUsageHeaderSnapshot(ctx context.Context, snapshot UsageHeaderSnapshot) bool {
	if s == nil {
		return false
	}
	authType := strings.ToLower(strings.TrimSpace(snapshot.AuthType))
	authIndex := strings.TrimSpace(snapshot.AuthIndex)
	if authType != "oauth" || authIndex == "" {
		return false
	}
	identity, err := repository.GetActiveAuthFileUsageIdentityByAuthIndex(ctx, s.db, authIndex)
	if err != nil {
		logUsageHeaderIdentityLookupError(authIndex, err)
		return false
	}
	if !usageHeaderIdentityIsCodex(identity) {
		return false
	}
	return s.applyUsageHeaderSnapshotWithIdentity(ctx, snapshot, identity, nil)
}

func logUsageHeaderIdentityLookupError(authIndex string, err error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return
	}
	logrus.WithError(err).WithField("auth_index", authIndex).Warn("usage header quota identity lookup failed")
}

func (s *Service) applyUsageHeaderSnapshotWithIdentity(ctx context.Context, snapshot UsageHeaderSnapshot, identity entities.UsageIdentity, statsProvider usageWindowStatsProvider) bool {
	if s == nil {
		return false
	}
	authType := strings.ToLower(strings.TrimSpace(snapshot.AuthType))
	authIndex := strings.TrimSpace(snapshot.AuthIndex)
	if authType != "oauth" || authIndex == "" || strings.TrimSpace(identity.Identity) != authIndex {
		return false
	}
	if !usageHeaderIdentityIsCodex(identity) {
		return false
	}
	output, ok := parseCodexHeaderQuota(snapshot.Headers)
	if !ok {
		return false
	}
	response := CheckResponse{
		ID:    authIndex,
		Quota: NormalizeQuotaRows(output),
	}
	if len(response.Quota) == 0 {
		return false
	}
	if count, ok := rateLimitResetCreditsAvailableCount(output); ok {
		response.RateLimitResetCreditsAvailableCount = count
	}
	observedAt := timeutil.NormalizeStorageTime(snapshot.ObservedAt)
	if observedAt.IsZero() {
		observedAt = timeutil.NormalizeStorageTime(time.Now())
	}
	if !s.shouldProcessUsageHeaderQuotaSnapshot(authIndex, observedAt) {
		return false
	}
	if statsProvider != nil {
		response = s.attachWindowUsageStatsWithProvider(ctx, authIndex, response, observedAt, statsProvider)
	} else {
		response = s.attachWindowUsageStats(ctx, authIndex, response, observedAt)
	}
	return s.mergeUsageHeaderQuotaCache(authIndex, response, observedAt, identity)
}

func usageHeaderIdentityIsCodex(identity entities.UsageIdentity) bool {
	return normalizeIdentityType(identity.Type) == "codex"
}

func (s *Service) shouldProcessUsageHeaderQuotaSnapshot(authIndex string, observedAt time.Time) bool {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	existing, ok := s.refreshTasks[authIndex]
	if !ok {
		return true
	}
	if existing.isActive() {
		return false
	}
	if usageHeaderCompletedCacheIsCurrentOrNewer(existing, observedAt) {
		return false
	}
	return true
}

func (s *Service) mergeUsageHeaderQuotaCache(authIndex string, response CheckResponse, observedAt time.Time, identity entities.UsageIdentity) bool {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	existing, ok := s.refreshTasks[authIndex]
	if ok {
		if existing.isActive() {
			return false
		}
		if usageHeaderCompletedCacheIsCurrentOrNewer(existing, observedAt) {
			return false
		}
		if existing.Quota != nil {
			response = mergeUsageHeaderQuotaResponse(*existing.Quota, response)
		}
	}
	s.refreshTasks[authIndex] = &RefreshTaskRecord{
		AuthIndex:   authIndex,
		Name:        identity.Name,
		Type:        identity.Type,
		FileName:    identity.FileName,
		Status:      RefreshTaskStatusCompleted,
		Quota:       &response,
		Source:      RefreshSourceUsageHeader,
		CreatedAt:   observedAt,
		RefreshedAt: observedAt,
	}
	return true
}

func usageHeaderCompletedCacheIsCurrentOrNewer(existing *RefreshTaskRecord, observedAt time.Time) bool {
	return existing != nil &&
		existing.Status == RefreshTaskStatusCompleted &&
		!existing.RefreshedAt.IsZero() &&
		!existing.RefreshedAt.Before(observedAt)
}

func mergeUsageHeaderQuotaResponse(existing CheckResponse, header CheckResponse) CheckResponse {
	merged := header
	if merged.ID == "" {
		merged.ID = existing.ID
	}
	if merged.RateLimitResetCreditsAvailableCount == nil {
		merged.RateLimitResetCreditsAvailableCount = existing.RateLimitResetCreditsAvailableCount
	}
	merged.Quota = mergeUsageHeaderQuotaRows(existing.Quota, header.Quota)
	return merged
}

func mergeUsageHeaderQuotaRows(existing []QuotaRow, header []QuotaRow) []QuotaRow {
	headerByKey := make(map[string]QuotaRow, len(header))
	headerOrder := make([]string, 0, len(header))
	for _, row := range header {
		if strings.TrimSpace(row.Key) == "" {
			continue
		}
		if _, ok := headerByKey[row.Key]; !ok {
			headerOrder = append(headerOrder, row.Key)
		}
		headerByKey[row.Key] = row
	}
	merged := make([]QuotaRow, 0, len(existing)+len(header))
	seen := make(map[string]struct{}, len(existing)+len(header))
	for _, row := range existing {
		if replacement, ok := headerByKey[row.Key]; ok {
			merged = append(merged, mergeUsageHeaderQuotaRow(row, replacement))
			seen[row.Key] = struct{}{}
			continue
		}
		merged = append(merged, row)
		if strings.TrimSpace(row.Key) != "" {
			seen[row.Key] = struct{}{}
		}
	}
	for _, key := range headerOrder {
		if _, ok := seen[key]; ok {
			continue
		}
		merged = append(merged, headerByKey[key])
	}
	return merged
}

func mergeUsageHeaderQuotaRow(existing QuotaRow, header QuotaRow) QuotaRow {
	merged := existing
	if strings.TrimSpace(header.Key) != "" {
		merged.Key = header.Key
	}
	if strings.TrimSpace(header.Label) != "" {
		merged.Label = header.Label
	}
	if strings.TrimSpace(header.Scope) != "" {
		merged.Scope = header.Scope
	}
	if strings.TrimSpace(header.Metric) != "" {
		merged.Metric = header.Metric
	}
	if strings.TrimSpace(header.PlanType) != "" {
		merged.PlanType = header.PlanType
	}
	if header.Used != nil {
		merged.Used = header.Used
	}
	if header.Limit != nil {
		merged.Limit = header.Limit
	}
	if header.Remaining != nil {
		merged.Remaining = header.Remaining
	}
	if header.RemainingFraction != nil {
		merged.RemainingFraction = header.RemainingFraction
	}
	if header.UsedPercent != nil {
		merged.UsedPercent = header.UsedPercent
	}
	if header.Allowed != nil {
		merged.Allowed = header.Allowed
	}
	if header.LimitReached != nil {
		merged.LimitReached = header.LimitReached
	}
	if header.Window != nil {
		merged.Window = header.Window
	}
	if strings.TrimSpace(header.ResetAt) != "" {
		merged.ResetAt = header.ResetAt
	}
	if header.ResetAfterSeconds != nil {
		merged.ResetAfterSeconds = header.ResetAfterSeconds
	}
	if usageHeaderQuotaRowIsWindow(header) {
		// Header 只负责刷新普通 window 进度条；token/cost 跟随同一窗口重新计算，即使为空也要清掉旧值。
		merged.WindowUsageTokens = header.WindowUsageTokens
		merged.WindowUsageCost = header.WindowUsageCost
	} else {
		if header.WindowUsageTokens != nil {
			merged.WindowUsageTokens = header.WindowUsageTokens
		}
		if header.WindowUsageCost != nil {
			merged.WindowUsageCost = header.WindowUsageCost
		}
	}
	return merged
}

func usageHeaderQuotaRowIsWindow(row QuotaRow) bool {
	return strings.EqualFold(strings.TrimSpace(row.Scope), "window")
}

func cloneUsageHeaderSnapshots(snapshots []UsageHeaderSnapshot) []UsageHeaderSnapshot {
	cloned := make([]UsageHeaderSnapshot, 0, len(snapshots))
	for _, snapshot := range snapshots {
		snapshot.Headers = cloneUsageHeaderHTTPHeaders(snapshot.Headers)
		cloned = append(cloned, snapshot)
	}
	return cloned
}

func cloneUsageHeaderHTTPHeaders(headers http.Header) http.Header {
	if len(headers) == 0 {
		return nil
	}
	cloned := make(http.Header, len(headers))
	for key, values := range headers {
		copied := make([]string, len(values))
		copy(copied, values)
		cloned[key] = copied
	}
	return cloned
}
