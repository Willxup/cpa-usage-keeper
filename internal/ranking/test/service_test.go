package test

import (
	"context"
	"crypto/rand"
	"errors"
	"sync"
	"testing"
	"time"

	"cpa-usage-keeper/internal/ranking"
)

type centerStub struct {
	register    func(context.Context, ranking.RegistrationCommand) (ranking.ParticipantInfo, error)
	self        func(context.Context, ranking.Credentials, time.Time) (ranking.SelfStatus, error)
	report      func(context.Context, ranking.ReportCommand) (ranking.ReportReceipt, error)
	delete      func(context.Context, ranking.DeleteCommand) (ranking.DeleteReceipt, error)
	leaderboard func(context.Context, ranking.LeaderboardPeriod, ranking.LeaderboardMetric) (ranking.Leaderboard, error)
	metadata    func(context.Context) (ranking.LeaderboardMetadata, error)

	mu               sync.Mutex
	registerCalls    int
	selfCalls        int
	reportCalls      int
	deleteCalls      int
	leaderboardCalls int
	metadataCalls    int
	registrations    []ranking.RegistrationCommand
	reports          []ranking.ReportCommand
	deletes          []ranking.DeleteCommand
}

func (s *centerStub) Register(ctx context.Context, command ranking.RegistrationCommand) (ranking.ParticipantInfo, error) {
	s.mu.Lock()
	s.registerCalls++
	s.registrations = append(s.registrations, command)
	s.mu.Unlock()
	if s.register != nil {
		return s.register(ctx, command)
	}
	return ranking.ParticipantInfo{ParticipantID: "p_example", DisplayName: command.DisplayName, AvatarID: command.AvatarID, CreatedAt: time.Now().UTC()}, nil
}

func (s *centerStub) Self(ctx context.Context, credentials ranking.Credentials, requestedAt time.Time) (ranking.SelfStatus, error) {
	s.mu.Lock()
	s.selfCalls++
	s.mu.Unlock()
	if s.self != nil {
		return s.self(ctx, credentials, requestedAt)
	}
	return ranking.SelfStatus{ParticipantID: credentials.ParticipantID, DisplayName: "Keeper_01", AvatarID: 7, Status: "active"}, nil
}

func (s *centerStub) SubmitReport(ctx context.Context, command ranking.ReportCommand) (ranking.ReportReceipt, error) {
	s.mu.Lock()
	s.reportCalls++
	s.reports = append(s.reports, command)
	s.mu.Unlock()
	if s.report != nil {
		return s.report(ctx, command)
	}
	return ranking.ReportReceipt{ParticipantID: command.ParticipantID, Sequence: command.Sequence, SnapshotAt: command.SnapshotAt, SyncedAt: command.SnapshotAt}, nil
}

func (s *centerStub) Delete(ctx context.Context, command ranking.DeleteCommand) (ranking.DeleteReceipt, error) {
	s.mu.Lock()
	s.deleteCalls++
	s.deletes = append(s.deletes, command)
	s.mu.Unlock()
	if s.delete != nil {
		return s.delete(ctx, command)
	}
	return ranking.DeleteReceipt{ParticipantID: command.ParticipantID, Sequence: command.Sequence, DeletedAt: command.RequestedAt}, nil
}

func (s *centerStub) Leaderboard(ctx context.Context, period ranking.LeaderboardPeriod, metric ranking.LeaderboardMetric) (ranking.Leaderboard, error) {
	s.mu.Lock()
	s.leaderboardCalls++
	s.mu.Unlock()
	if s.leaderboard != nil {
		return s.leaderboard(ctx, period, metric)
	}
	return ranking.Leaderboard{Period: period, Metric: metric}, nil
}

func (s *centerStub) LeaderboardMetadata(ctx context.Context) (ranking.LeaderboardMetadata, error) {
	s.mu.Lock()
	s.metadataCalls++
	s.mu.Unlock()
	if s.metadata != nil {
		return s.metadata(ctx)
	}
	return ranking.LeaderboardMetadata{PeriodTimezone: "Asia/Shanghai"}, nil
}

type aggregateRange struct {
	start time.Time
	end   time.Time
}

type aggregatorStub struct {
	metrics     ranking.Metrics
	latestID    int64
	latestByDay map[string]int64
	err         error
	mu          sync.Mutex
	ranges      []aggregateRange
	latest      int
}

func (s *aggregatorStub) AggregateDay(_ context.Context, start, end time.Time) (ranking.Metrics, error) {
	s.mu.Lock()
	s.ranges = append(s.ranges, aggregateRange{start: start, end: end})
	s.mu.Unlock()
	if s.err != nil {
		return ranking.Metrics{}, s.err
	}
	return s.metrics, nil
}

func (s *aggregatorStub) LatestEventID(_ context.Context, start, _ time.Time) (int64, error) {
	s.mu.Lock()
	s.latest++
	s.mu.Unlock()
	if s.err != nil {
		return 0, s.err
	}
	if s.latestByDay != nil {
		return s.latestByDay[start.Format("2006-01-02")], nil
	}
	return s.latestID, nil
}

func TestServiceDisabledStateNeverCallsCenterOrAggregator(t *testing.T) {
	store := ranking.NewStore(openRankingDatabase(t))
	center := &centerStub{}
	aggregator := &aggregatorStub{}
	service, err := ranking.NewService(store, aggregator, center)
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}

	if err := service.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce returned error: %v", err)
	}
	if center.registerCalls != 0 || center.selfCalls != 0 || center.reportCalls != 0 || aggregator.latest != 0 || len(aggregator.ranges) != 0 {
		t.Fatalf("disabled ranking made external or aggregate calls: center=%+v aggregator=%+v", center, aggregator)
	}
}

func TestServiceSkipsZeroEventDaysWithoutUploading(t *testing.T) {
	store := ranking.NewStore(openRankingDatabase(t))
	seedActiveState(t, store, 0)
	now := time.Date(2026, 7, 23, 17, 15, 0, 0, time.UTC)
	center := &centerStub{}
	center.self = func(_ context.Context, credentials ranking.Credentials, _ time.Time) (ranking.SelfStatus, error) {
		return ranking.SelfStatus{ParticipantID: credentials.ParticipantID, DisplayName: "Keeper_01", AvatarID: 7, Status: "active"}, nil
	}
	aggregator := &aggregatorStub{latestByDay: map[string]int64{"2026-07-23": 0, "2026-07-24": 0}}
	service, err := ranking.NewService(store, aggregator, center, ranking.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}

	if err := service.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce returned error: %v", err)
	}
	if center.reportCalls != 0 || len(aggregator.ranges) != 0 {
		t.Fatalf("zero-event days were uploaded or aggregated: reports=%d ranges=%+v", center.reportCalls, aggregator.ranges)
	}
	state, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("load zero-event state: %v", err)
	}
	if state.LastSuccessfulCompleteDay != "2026-07-23" || state.LastSuccessfulCompleteEventID != 0 || state.LastAllocatedSequence != 0 {
		t.Fatalf("zero-event day watermark = %+v", state)
	}

	if err := service.RunOnce(context.Background()); err != nil {
		t.Fatalf("second RunOnce returned error: %v", err)
	}
	if center.reportCalls != 0 || len(aggregator.ranges) != 0 {
		t.Fatalf("unchanged zero-event days caused work: reports=%d ranges=%+v", center.reportCalls, aggregator.ranges)
	}
}

func TestServiceReconcilesCenterDeletionWhenUsageIsUnchanged(t *testing.T) {
	store := ranking.NewStore(openRankingDatabase(t))
	seedActiveState(t, store, 0)
	now := time.Date(2026, 7, 24, 9, 0, 0, 0, time.UTC)
	center := &centerStub{}
	center.self = func(_ context.Context, credentials ranking.Credentials, _ time.Time) (ranking.SelfStatus, error) {
		status := "active"
		if center.selfCalls > 1 {
			status = "deleted"
		}
		return ranking.SelfStatus{
			ParticipantID: credentials.ParticipantID,
			DisplayName:   "Keeper_01",
			AvatarID:      7,
			Status:        status,
		}, nil
	}
	service, err := ranking.NewService(
		store,
		&aggregatorStub{latestID: 0},
		center,
		ranking.WithClock(func() time.Time { return now }),
	)
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}

	if err := service.RunOnce(context.Background()); err != nil {
		t.Fatalf("first RunOnce returned error: %v", err)
	}
	if err := service.RunOnce(context.Background()); !errors.Is(err, ranking.ErrParticipantDeleted) {
		t.Fatalf("second RunOnce error = %v, want participant deleted", err)
	}
	state, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("load reconciled state: %v", err)
	}
	if center.selfCalls != 2 || center.reportCalls != 0 || state.Status != ranking.StatusDeleted {
		t.Fatalf("unchanged usage did not converge deletion: self=%d reports=%d state=%+v", center.selfCalls, center.reportCalls, state)
	}
}

func TestServicePauseStopsSyncAndResumeKeepsIdentity(t *testing.T) {
	store := ranking.NewStore(openRankingDatabase(t))
	seedActiveState(t, store, 4)
	before, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("load active state: %v", err)
	}
	now := time.Date(2026, 7, 24, 9, 0, 0, 0, time.UTC)
	center := &centerStub{}
	center.self = func(_ context.Context, credentials ranking.Credentials, _ time.Time) (ranking.SelfStatus, error) {
		return ranking.SelfStatus{ParticipantID: credentials.ParticipantID, DisplayName: "Keeper_01", AvatarID: 7, Status: "active", LastSequence: 4}, nil
	}
	aggregator := &aggregatorStub{latestID: 9, metrics: ranking.Metrics{RequestCount: 1, SuccessCount: 1, TotalTokens: 10}}
	service, err := ranking.NewService(store, aggregator, center, ranking.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}

	paused, err := service.Pause(context.Background())
	if err != nil {
		t.Fatalf("Pause returned error: %v", err)
	}
	if paused.Status != ranking.StatusPaused || paused.ParticipantID != before.ParticipantID || paused.DisplayName != before.DisplayName || paused.AvatarID != before.AvatarID {
		t.Fatalf("paused status changed identity: before=%+v after=%+v", before, paused)
	}
	if err := service.RunOnce(context.Background()); err != nil {
		t.Fatalf("paused RunOnce returned error: %v", err)
	}
	if err := service.SyncNow(context.Background()); !errors.Is(err, ranking.ErrParticipation) {
		t.Fatalf("paused SyncNow error = %v, want participation conflict", err)
	}
	if center.selfCalls != 0 || center.reportCalls != 0 || aggregator.latest != 0 || len(aggregator.ranges) != 0 {
		t.Fatalf("paused state performed synchronization: center=%+v aggregator=%+v", center, aggregator)
	}

	active, err := service.Resume(context.Background())
	if err != nil {
		t.Fatalf("Resume returned error: %v", err)
	}
	if active.Status != ranking.StatusActive || active.ParticipantID != before.ParticipantID || active.DisplayName != before.DisplayName || active.AvatarID != before.AvatarID {
		t.Fatalf("resumed status changed identity: before=%+v after=%+v", before, active)
	}
	if center.selfCalls != 0 || center.reportCalls != 0 || aggregator.latest != 0 {
		t.Fatalf("Resume triggered synchronization instead of only enabling it: center=%+v aggregator=%+v", center, aggregator)
	}
	if err := service.RunOnce(context.Background()); err != nil {
		t.Fatalf("resumed RunOnce returned error: %v", err)
	}
	if center.selfCalls != 1 || center.reportCalls != 1 || aggregator.latest != 1 || len(aggregator.ranges) != 1 {
		t.Fatalf("resumed state did not synchronize normally: center=%+v aggregator=%+v", center, aggregator)
	}
}

func TestServicePausedParticipantCanExitPermanently(t *testing.T) {
	store := ranking.NewStore(openRankingDatabase(t))
	seedActiveState(t, store, 2)
	now := time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC)
	center := &centerStub{}
	center.self = func(_ context.Context, credentials ranking.Credentials, _ time.Time) (ranking.SelfStatus, error) {
		return ranking.SelfStatus{ParticipantID: credentials.ParticipantID, DisplayName: "Keeper_01", AvatarID: 7, Status: "active", LastSequence: 2}, nil
	}
	service, err := ranking.NewService(store, &aggregatorStub{}, center, ranking.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}
	if _, err := service.Pause(context.Background()); err != nil {
		t.Fatalf("Pause returned error: %v", err)
	}
	status, err := service.Exit(context.Background())
	if err != nil {
		t.Fatalf("Exit from paused state returned error: %v", err)
	}
	if status.Status != ranking.StatusDeleted || center.deleteCalls != 1 {
		t.Fatalf("paused exit result = %+v, delete calls=%d", status, center.deleteCalls)
	}
}

func TestServiceCachesLeaderboardsAndMetadataForThirtySeconds(t *testing.T) {
	store := ranking.NewStore(openRankingDatabase(t))
	now := time.Date(2026, 7, 24, 9, 0, 0, 0, time.UTC)
	center := &centerStub{}
	service, err := ranking.NewService(store, &aggregatorStub{}, center, ranking.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}

	for range 2 {
		if _, err := service.Leaderboard(context.Background(), ranking.LeaderboardToday, ranking.MetricOverall); err != nil {
			t.Fatalf("Leaderboard returned error: %v", err)
		}
		if _, err := service.LeaderboardMetadata(context.Background()); err != nil {
			t.Fatalf("LeaderboardMetadata returned error: %v", err)
		}
	}
	if center.leaderboardCalls != 1 || center.metadataCalls != 1 {
		t.Fatalf("expected one center read inside cache TTL, leaderboard=%d metadata=%d", center.leaderboardCalls, center.metadataCalls)
	}

	now = now.Add(31 * time.Second)
	if _, err := service.Leaderboard(context.Background(), ranking.LeaderboardToday, ranking.MetricOverall); err != nil {
		t.Fatalf("expired Leaderboard returned error: %v", err)
	}
	if _, err := service.LeaderboardMetadata(context.Background()); err != nil {
		t.Fatalf("expired LeaderboardMetadata returned error: %v", err)
	}
	if center.leaderboardCalls != 2 || center.metadataCalls != 2 {
		t.Fatalf("expected cache refresh after TTL, leaderboard=%d metadata=%d", center.leaderboardCalls, center.metadataCalls)
	}
}

func TestServiceRejectsUnsupportedProfileWithoutPersistingIdentity(t *testing.T) {
	for _, name := range []string{"Keeper.Name", "Keeper Name", "Keeper😀", "123-456-7890", "١٢٣٤٥٦٧٨٩٠", "abcdefghijklmnopq"} {
		t.Run(name, func(t *testing.T) {
			store := ranking.NewStore(openRankingDatabase(t))
			center := &centerStub{}
			service, err := ranking.NewService(store, &aggregatorStub{}, center)
			if err != nil {
				t.Fatalf("NewService returned error: %v", err)
			}
			if _, err := service.Join(context.Background(), name, 7); !errors.Is(err, ranking.ErrInvalidProfile) {
				t.Fatalf("expected invalid profile error for %q, got %v", name, err)
			}
			state, err := store.Load(context.Background())
			if err != nil {
				t.Fatalf("Load returned error: %v", err)
			}
			if state.Status != ranking.StatusDisabled || center.registerCalls != 0 {
				t.Fatalf("invalid profile created identity or center call: state=%+v calls=%d", state, center.registerCalls)
			}
		})
	}
}

func TestServiceAcceptsSixteenCharacterProfile(t *testing.T) {
	store := ranking.NewStore(openRankingDatabase(t))
	center := &centerStub{}
	service, err := ranking.NewService(store, &aggregatorStub{}, center)
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}

	status, err := service.Join(context.Background(), "abcdefghijklmnop", 7)
	if err != nil {
		t.Fatalf("Join returned error for 16-character profile: %v", err)
	}
	if status.DisplayName != "abcdefghijklmnop" || center.registerCalls != 1 {
		t.Fatalf("16-character profile was not registered: status=%+v calls=%d", status, center.registerCalls)
	}
}

func TestServicePersistsJoiningBeforeRegistrationAndRetriesSameIdentity(t *testing.T) {
	store := ranking.NewStore(openRankingDatabase(t))
	now := time.Date(2026, 7, 24, 8, 30, 0, 0, time.UTC)
	center := &centerStub{}
	first := true
	center.register = func(ctx context.Context, command ranking.RegistrationCommand) (ranking.ParticipantInfo, error) {
		persisted, err := store.Load(ctx)
		if err != nil {
			t.Fatalf("load state during registration: %v", err)
		}
		if persisted.Status != ranking.StatusJoining || persisted.PublicKey != command.PublicKey || persisted.RegistrationIdempotencyKey != command.IdempotencyKey {
			t.Fatalf("joining identity was not persisted before registration: state=%+v command=%+v", persisted, command)
		}
		if first {
			first = false
			return ranking.ParticipantInfo{}, errors.New("temporary center failure")
		}
		return ranking.ParticipantInfo{ParticipantID: "p_example", DisplayName: command.DisplayName, AvatarID: command.AvatarID, CreatedAt: now}, nil
	}
	center.report = func(ctx context.Context, command ranking.ReportCommand) (ranking.ReportReceipt, error) {
		persisted, err := store.Load(ctx)
		if err != nil {
			t.Fatalf("load state during report: %v", err)
		}
		if persisted.LastAllocatedSequence != command.Sequence {
			t.Fatalf("sequence was not persisted before report: state=%+v command=%+v", persisted, command)
		}
		return ranking.ReportReceipt{ParticipantID: command.ParticipantID, Sequence: command.Sequence, SnapshotAt: command.SnapshotAt, SyncedAt: now}, nil
	}
	aggregator := &aggregatorStub{latestID: 42, metrics: ranking.Metrics{RequestCount: 1, SuccessCount: 1, TotalTokens: 10}}
	service, err := ranking.NewService(store, aggregator, center, ranking.WithClock(func() time.Time { return now }), ranking.WithRandom(rand.Reader))
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}

	if _, err := service.Join(context.Background(), "Keeper_01", 7); err == nil {
		t.Fatal("expected first registration attempt to fail")
	}
	joining, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("load joining state: %v", err)
	}
	if joining.Status != ranking.StatusJoining || joining.LastError == "" {
		t.Fatalf("expected recoverable joining state, got %+v", joining)
	}
	if err := service.RunOnce(context.Background()); err != nil {
		t.Fatalf("joining RunOnce returned error: %v", err)
	}
	if center.registerCalls != 1 {
		t.Fatalf("background synchronization retried registration: calls=%d", center.registerCalls)
	}

	status, err := service.Join(context.Background(), "Keeper_01", 7)
	if err != nil {
		t.Fatalf("retry Join returned error: %v", err)
	}
	if status.Status != ranking.StatusActive || status.ParticipantID != "p_example" || center.reportCalls != 1 {
		t.Fatalf("unexpected joined status or immediate sync: status=%+v reports=%d", status, center.reportCalls)
	}
	if len(center.registrations) != 2 || center.registrations[0].PublicKey != center.registrations[1].PublicKey || center.registrations[0].PrivateKey != center.registrations[1].PrivateKey || center.registrations[0].IdempotencyKey != center.registrations[1].IdempotencyKey {
		t.Fatalf("registration retry did not reuse identity: %+v", center.registrations)
	}
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load Shanghai location: %v", err)
	}
	if len(aggregator.ranges) != 1 || aggregator.ranges[0].start.Location().String() != "Asia/Shanghai" || !aggregator.ranges[0].start.Equal(time.Date(2026, 7, 24, 0, 0, 0, 0, shanghai)) || !aggregator.ranges[0].end.Equal(now) {
		t.Fatalf("unexpected immediate current-day aggregation: %+v", aggregator.ranges)
	}
}

func TestServiceExitFromJoiningRegistersOnlyToDeleteWithoutUploading(t *testing.T) {
	store := ranking.NewStore(openRankingDatabase(t))
	now := time.Date(2026, 7, 24, 8, 30, 0, 0, time.UTC)
	center := &centerStub{}
	first := true
	center.register = func(_ context.Context, command ranking.RegistrationCommand) (ranking.ParticipantInfo, error) {
		if first {
			first = false
			return ranking.ParticipantInfo{}, errors.New("temporary center failure")
		}
		return ranking.ParticipantInfo{ParticipantID: "p_example", DisplayName: command.DisplayName, AvatarID: command.AvatarID, CreatedAt: now}, nil
	}
	service, err := ranking.NewService(
		store,
		&aggregatorStub{latestID: 42, metrics: ranking.Metrics{RequestCount: 1, SuccessCount: 1}},
		center,
		ranking.WithClock(func() time.Time { return now }),
	)
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}

	if _, err := service.Join(context.Background(), "Keeper_01", 7); err == nil {
		t.Fatal("expected first registration attempt to fail")
	}
	status, err := service.Exit(context.Background())
	if err != nil {
		t.Fatalf("Exit from joining returned error: %v", err)
	}
	if status.Status != ranking.StatusDeleted || center.registerCalls != 2 || center.deleteCalls != 1 {
		t.Fatalf("unexpected joining exit: status=%+v register=%d delete=%d", status, center.registerCalls, center.deleteCalls)
	}
	if center.reportCalls != 0 {
		t.Fatalf("joining exit uploaded usage before deletion: reports=%+v", center.reports)
	}
}

func TestServiceReconcilesSequenceThenSubmitsNextNumber(t *testing.T) {
	store := ranking.NewStore(openRankingDatabase(t))
	seedActiveState(t, store, 5)
	now := time.Date(2026, 7, 24, 9, 0, 0, 0, time.UTC)
	center := &centerStub{}
	center.self = func(_ context.Context, credentials ranking.Credentials, _ time.Time) (ranking.SelfStatus, error) {
		return ranking.SelfStatus{ParticipantID: credentials.ParticipantID, DisplayName: "Keeper_01", AvatarID: 7, Status: "active", LastSequence: 10}, nil
	}
	center.report = func(ctx context.Context, command ranking.ReportCommand) (ranking.ReportReceipt, error) {
		persisted, err := store.Load(ctx)
		if err != nil {
			t.Fatalf("load allocated state: %v", err)
		}
		if command.Sequence != 11 || persisted.LastAllocatedSequence != 11 {
			t.Fatalf("expected reconciled sequence 11, state=%+v command=%+v", persisted, command)
		}
		return ranking.ReportReceipt{ParticipantID: command.ParticipantID, Sequence: command.Sequence, SnapshotAt: command.SnapshotAt, SyncedAt: now}, nil
	}
	service, err := ranking.NewService(store, &aggregatorStub{latestID: 1}, center, ranking.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}

	if err := service.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce returned error: %v", err)
	}
	if center.selfCalls != 1 || center.reportCalls != 1 {
		t.Fatalf("expected one self and one report call, got self=%d report=%d", center.selfCalls, center.reportCalls)
	}
}

func TestServiceReconcilesAgainAfterReportSequenceReplay(t *testing.T) {
	store := ranking.NewStore(openRankingDatabase(t))
	seedActiveState(t, store, 5)
	now := time.Date(2026, 7, 24, 9, 0, 0, 0, time.UTC)
	center := &centerStub{}
	center.self = func(_ context.Context, credentials ranking.Credentials, _ time.Time) (ranking.SelfStatus, error) {
		lastSequence := int64(5)
		if center.selfCalls > 1 {
			lastSequence = 10
		}
		return ranking.SelfStatus{ParticipantID: credentials.ParticipantID, DisplayName: "Keeper_01", AvatarID: 7, Status: "active", LastSequence: lastSequence}, nil
	}
	center.report = func(_ context.Context, command ranking.ReportCommand) (ranking.ReportReceipt, error) {
		if center.reportCalls == 1 {
			return ranking.ReportReceipt{}, ranking.ErrReplayedSequence
		}
		return ranking.ReportReceipt{ParticipantID: command.ParticipantID, Sequence: command.Sequence, SnapshotAt: command.SnapshotAt, SyncedAt: now}, nil
	}
	service, err := ranking.NewService(store, &aggregatorStub{latestID: 1}, center, ranking.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}

	if err := service.RunOnce(context.Background()); !errors.Is(err, ranking.ErrReplayedSequence) {
		t.Fatalf("expected first sequence replay error, got %v", err)
	}
	if err := service.RunOnce(context.Background()); err != nil {
		t.Fatalf("reconciled RunOnce returned error: %v", err)
	}
	if center.selfCalls != 2 || len(center.reports) != 2 || center.reports[0].Sequence != 6 || center.reports[1].Sequence != 11 {
		t.Fatalf("unexpected replay recovery: self=%d reports=%+v", center.selfCalls, center.reports)
	}
}

func TestServicePersistsDeletedTombstoneOnCenterGone(t *testing.T) {
	store := ranking.NewStore(openRankingDatabase(t))
	seedActiveState(t, store, 0)
	now := time.Date(2026, 7, 24, 9, 0, 0, 0, time.UTC)
	center := &centerStub{}
	center.self = func(_ context.Context, credentials ranking.Credentials, _ time.Time) (ranking.SelfStatus, error) {
		return ranking.SelfStatus{ParticipantID: credentials.ParticipantID, DisplayName: "Keeper_01", AvatarID: 7, Status: "active"}, nil
	}
	center.report = func(context.Context, ranking.ReportCommand) (ranking.ReportReceipt, error) {
		return ranking.ReportReceipt{}, ranking.ErrParticipantDeleted
	}
	service, err := ranking.NewService(store, &aggregatorStub{latestID: 1}, center, ranking.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}

	if err := service.RunOnce(context.Background()); !errors.Is(err, ranking.ErrParticipantDeleted) {
		t.Fatalf("expected participant deleted error, got %v", err)
	}
	state, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("load deleted state: %v", err)
	}
	if state.Status != ranking.StatusDeleted {
		t.Fatalf("expected deleted tombstone, got %+v", state)
	}
	beforeSelf, beforeReports := center.selfCalls, center.reportCalls
	if err := service.RunOnce(context.Background()); err != nil {
		t.Fatalf("deleted RunOnce returned error: %v", err)
	}
	if center.selfCalls != beforeSelf || center.reportCalls != beforeReports {
		t.Fatal("deleted state continued calling ranking center")
	}
	if _, err := service.Join(context.Background(), "Keeper_01", 7); !errors.Is(err, ranking.ErrDeletedState) {
		t.Fatalf("expected deleted state to reject rejoin, got %v", err)
	}
}

func TestServiceUsesCenterDayBoundaryRegardlessOfKeeperClockLocation(t *testing.T) {
	store := ranking.NewStore(openRankingDatabase(t))
	seedActiveState(t, store, 0)
	nowUTC := time.Date(2026, 7, 23, 16, 15, 0, 0, time.UTC)
	now := nowUTC.In(time.FixedZone("KeeperConfiguredTimezone", -5*60*60))
	center := &centerStub{}
	center.metadata = func(context.Context) (ranking.LeaderboardMetadata, error) {
		return ranking.LeaderboardMetadata{PeriodTimezone: "Asia/Tokyo"}, nil
	}
	center.self = func(_ context.Context, credentials ranking.Credentials, _ time.Time) (ranking.SelfStatus, error) {
		return ranking.SelfStatus{ParticipantID: credentials.ParticipantID, DisplayName: "Keeper_01", AvatarID: 7, Status: "active"}, nil
	}
	aggregator := &aggregatorStub{latestID: 5, metrics: ranking.Metrics{RequestCount: 2, SuccessCount: 2}}
	service, err := ranking.NewService(store, aggregator, center, ranking.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}

	if err := service.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce returned error: %v", err)
	}
	if len(center.reports) != 2 || center.reports[0].PeriodTimezone != "Asia/Tokyo" || !center.reports[0].Complete || center.reports[0].DayKey != "2026-07-23" || center.reports[1].PeriodTimezone != "Asia/Tokyo" || center.reports[1].Complete || center.reports[1].DayKey != "2026-07-24" {
		t.Fatalf("unexpected report order: %+v", center.reports)
	}
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatalf("load Tokyo location: %v", err)
	}
	if len(aggregator.ranges) != 2 || aggregator.ranges[0].start.Location().String() != "Asia/Tokyo" || !aggregator.ranges[0].start.Equal(time.Date(2026, 7, 23, 0, 0, 0, 0, tokyo)) || !aggregator.ranges[0].end.Equal(time.Date(2026, 7, 24, 0, 0, 0, 0, tokyo)) || !aggregator.ranges[1].start.Equal(time.Date(2026, 7, 24, 0, 0, 0, 0, tokyo)) || !aggregator.ranges[1].end.Equal(nowUTC) {
		t.Fatalf("unexpected day ranges: %+v", aggregator.ranges)
	}
	state, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("load synced state: %v", err)
	}
	if state.LastSuccessfulCompleteDay != "2026-07-23" || state.LastAllocatedSequence != 2 {
		t.Fatalf("unexpected completed state: %+v", state)
	}
}

func TestServiceUsesCenterSyncIntervalWithSafetyFloor(t *testing.T) {
	tests := []struct {
		name            string
		requestedSecond int64
		want            time.Duration
	}{
		{name: "follows center interval", requestedSecond: 1200, want: 20 * time.Minute},
		{name: "clamps short interval", requestedSecond: 300, want: 15 * time.Minute},
		{name: "clamps zero interval", requestedSecond: 0, want: 15 * time.Minute},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := ranking.NewStore(openRankingDatabase(t))
			seedActiveState(t, store, 0)
			center := &centerStub{}
			center.metadata = func(context.Context) (ranking.LeaderboardMetadata, error) {
				return ranking.LeaderboardMetadata{SuggestedSyncIntervalSeconds: test.requestedSecond}, nil
			}
			service, err := ranking.NewService(store, &aggregatorStub{}, center)
			if err != nil {
				t.Fatalf("NewService returned error: %v", err)
			}

			interval, err := service.SyncInterval(context.Background())
			if err != nil {
				t.Fatalf("SyncInterval returned error: %v", err)
			}
			if interval != test.want {
				t.Fatalf("SyncInterval = %v, want %v", interval, test.want)
			}
		})
	}
}

func TestServiceDoesNotFetchCenterIntervalUntilParticipantIsActive(t *testing.T) {
	center := &centerStub{}
	center.metadata = func(context.Context) (ranking.LeaderboardMetadata, error) {
		t.Fatal("disabled participant fetched ranking metadata")
		return ranking.LeaderboardMetadata{}, nil
	}
	service, err := ranking.NewService(ranking.NewStore(openRankingDatabase(t)), &aggregatorStub{}, center)
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}

	interval, err := service.SyncInterval(context.Background())
	if err != nil {
		t.Fatalf("SyncInterval returned error: %v", err)
	}
	if interval != 30*time.Minute {
		t.Fatalf("disabled SyncInterval = %v, want 30m", interval)
	}
}

func TestServiceResubmitsYesterdayOnlyWhenLateEventsAdvanceWatermark(t *testing.T) {
	store := ranking.NewStore(openRankingDatabase(t))
	seedActiveState(t, store, 0)
	now := time.Date(2026, 7, 23, 17, 15, 0, 0, time.UTC)
	center := &centerStub{}
	center.self = func(_ context.Context, credentials ranking.Credentials, _ time.Time) (ranking.SelfStatus, error) {
		return ranking.SelfStatus{ParticipantID: credentials.ParticipantID, DisplayName: "Keeper_01", AvatarID: 7, Status: "active"}, nil
	}
	aggregator := &aggregatorStub{
		latestByDay: map[string]int64{"2026-07-23": 5, "2026-07-24": 10},
		metrics:     ranking.Metrics{RequestCount: 2, SuccessCount: 2},
	}
	service, err := ranking.NewService(store, aggregator, center, ranking.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}

	if err := service.RunOnce(context.Background()); err != nil {
		t.Fatalf("first RunOnce returned error: %v", err)
	}
	aggregator.latestByDay["2026-07-23"] = 6
	now = time.Date(2026, 7, 23, 17, 45, 0, 0, time.UTC)
	if err := service.RunOnce(context.Background()); err != nil {
		t.Fatalf("late-event RunOnce returned error: %v", err)
	}
	if len(center.reports) != 3 || !center.reports[2].Complete || center.reports[2].DayKey != "2026-07-23" {
		t.Fatalf("expected one replacement yesterday report, got %+v", center.reports)
	}
	state, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("load state after late event: %v", err)
	}
	if state.LastSuccessfulCompleteDay != "2026-07-23" || state.LastSuccessfulCompleteEventID != 6 {
		t.Fatalf("unexpected complete-day watermark: %+v", state)
	}

	now = time.Date(2026, 7, 23, 17, 50, 0, 0, time.UTC)
	if err := service.RunOnce(context.Background()); err != nil {
		t.Fatalf("unchanged-watermark RunOnce returned error: %v", err)
	}
	if len(center.reports) != 3 {
		t.Fatalf("unchanged yesterday watermark produced duplicate reports: %+v", center.reports)
	}
}

func TestServiceNewParticipantBeforeTwoShanghaiDoesNotBackfillYesterday(t *testing.T) {
	store := ranking.NewStore(openRankingDatabase(t))
	now := time.Date(2026, 7, 23, 17, 15, 0, 0, time.UTC)
	center := &centerStub{}
	center.register = func(_ context.Context, command ranking.RegistrationCommand) (ranking.ParticipantInfo, error) {
		return ranking.ParticipantInfo{
			ParticipantID: "p_new", DisplayName: command.DisplayName, AvatarID: command.AvatarID, CreatedAt: now,
		}, nil
	}
	aggregator := &aggregatorStub{latestID: 1}
	service, err := ranking.NewService(store, aggregator, center, ranking.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}

	if _, err := service.Join(context.Background(), "Keeper_New", 8); err != nil {
		t.Fatalf("Join returned error: %v", err)
	}
	if len(center.reports) != 1 || center.reports[0].Complete || center.reports[0].DayKey != "2026-07-24" {
		var complete bool
		var dayKey string
		if len(center.reports) > 0 {
			complete = center.reports[0].Complete
			dayKey = center.reports[0].DayKey
		}
		t.Fatalf("new participant report count=%d complete=%v day=%q", len(center.reports), complete, dayKey)
	}
}

func TestServiceExitAllocatesSequenceBeforeDeleteAndStopsPermanently(t *testing.T) {
	store := ranking.NewStore(openRankingDatabase(t))
	seedActiveState(t, store, 2)
	now := time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC)
	center := &centerStub{}
	center.delete = func(ctx context.Context, command ranking.DeleteCommand) (ranking.DeleteReceipt, error) {
		persisted, err := store.Load(ctx)
		if err != nil {
			t.Fatalf("load state during delete: %v", err)
		}
		if command.Sequence != 3 || persisted.LastAllocatedSequence != 3 {
			t.Fatalf("delete sequence was not persisted first: state=%+v command=%+v", persisted, command)
		}
		return ranking.DeleteReceipt{ParticipantID: command.ParticipantID, Sequence: command.Sequence, DeletedAt: now}, nil
	}
	service, err := ranking.NewService(store, &aggregatorStub{}, center, ranking.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}

	status, err := service.Exit(context.Background())
	if err != nil {
		t.Fatalf("Exit returned error: %v", err)
	}
	if status.Status != ranking.StatusDeleted || center.deleteCalls != 1 {
		t.Fatalf("unexpected exit result: status=%+v calls=%d", status, center.deleteCalls)
	}
}

func TestServiceExitReconcilesAndRetriesSequenceReplayOnce(t *testing.T) {
	store := ranking.NewStore(openRankingDatabase(t))
	seedActiveState(t, store, 2)
	now := time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC)
	center := &centerStub{}
	center.self = func(_ context.Context, credentials ranking.Credentials, _ time.Time) (ranking.SelfStatus, error) {
		lastSequence := int64(2)
		if center.selfCalls > 1 {
			lastSequence = 10
		}
		return ranking.SelfStatus{ParticipantID: credentials.ParticipantID, DisplayName: "Keeper_01", AvatarID: 7, Status: "active", LastSequence: lastSequence}, nil
	}
	center.delete = func(_ context.Context, command ranking.DeleteCommand) (ranking.DeleteReceipt, error) {
		if center.deleteCalls == 1 {
			return ranking.DeleteReceipt{}, ranking.ErrReplayedSequence
		}
		return ranking.DeleteReceipt{ParticipantID: command.ParticipantID, Sequence: command.Sequence, DeletedAt: now}, nil
	}
	service, err := ranking.NewService(store, &aggregatorStub{}, center, ranking.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}

	status, err := service.Exit(context.Background())
	if err != nil {
		t.Fatalf("Exit returned error: %v", err)
	}
	if status.Status != ranking.StatusDeleted || center.selfCalls != 2 || len(center.deletes) != 2 || center.deletes[0].Sequence != 3 || center.deletes[1].Sequence != 11 {
		t.Fatalf("unexpected delete replay recovery: status=%+v self=%d deletes=%+v", status, center.selfCalls, center.deletes)
	}
}

func TestServiceExitPersistsDeletedTombstoneAfterRequestCancellation(t *testing.T) {
	store := ranking.NewStore(openRankingDatabase(t))
	seedActiveState(t, store, 2)
	now := time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC)
	ctx, cancel := context.WithCancel(context.Background())
	center := &centerStub{}
	center.self = func(_ context.Context, credentials ranking.Credentials, _ time.Time) (ranking.SelfStatus, error) {
		return ranking.SelfStatus{ParticipantID: credentials.ParticipantID, DisplayName: "Keeper_01", AvatarID: 7, Status: "active", LastSequence: 2}, nil
	}
	center.delete = func(_ context.Context, command ranking.DeleteCommand) (ranking.DeleteReceipt, error) {
		cancel()
		return ranking.DeleteReceipt{ParticipantID: command.ParticipantID, Sequence: command.Sequence, DeletedAt: now}, nil
	}
	service, err := ranking.NewService(store, &aggregatorStub{}, center, ranking.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}

	status, err := service.Exit(ctx)
	if err != nil {
		t.Fatalf("Exit returned error after center success: %v", err)
	}
	if status.Status != ranking.StatusDeleted {
		t.Fatalf("expected deleted tombstone after caller cancellation, got %+v", status)
	}
	state, err := store.Load(context.Background())
	if err != nil || state.Status != ranking.StatusDeleted {
		t.Fatalf("deleted tombstone was not persisted: state=%+v err=%v", state, err)
	}
}

func seedActiveState(t *testing.T, store *ranking.Store, sequence int64) {
	t.Helper()
	identity, err := ranking.GenerateIdentity(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateIdentity returned error: %v", err)
	}
	if err := store.Save(context.Background(), ranking.State{
		Status: ranking.StatusActive, PublicKey: identity.PublicKey, PrivateKey: identity.PrivateKey,
		RegistrationIdempotencyKey: "registration_once", DisplayName: "Keeper_01", AvatarID: 7,
		ParticipantID: "p_example", ParticipationStartedAt: rankingTimePointer(time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)), LastAllocatedSequence: sequence,
	}); err != nil {
		t.Fatalf("seed active state: %v", err)
	}
}

func rankingTimePointer(value time.Time) *time.Time {
	return &value
}
