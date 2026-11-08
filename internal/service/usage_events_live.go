package service

import (
	"context"
	"time"

	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
	servicedto "cpa-usage-keeper/internal/service/dto"
)

// UsageEventsLiveProvider 只供请求事件流使用，不扩大普通 UsageProvider 合同。
type UsageEventsLiveProvider interface {
	LiveSnapshot(context.Context, servicedto.UsageFilter) (*servicedto.UsageEventsPage, int64, error)
	LiveCount(context.Context, servicedto.UsageFilter) (int64, error)
	LiveHighID(context.Context) (int64, error)
	LiveIDBatch(context.Context, servicedto.UsageFilter, int64, int) ([]servicedto.UsageEventRecord, int64, int, error)
	LiveTimeBatch(context.Context, servicedto.UsageFilter, int64, time.Time, time.Time, *time.Time, int64, int) ([]servicedto.UsageEventRecord, *time.Time, int64, int, error)
}

func (s *usageService) liveFilter(ctx context.Context, filter servicedto.UsageFilter) (repodto.UsageQueryFilter, error) {
	key, err := s.resolveAPIGroupKey(ctx, filter.APIKeyID)
	return repodto.UsageQueryFilter{
		Range: filter.Range, CustomUnit: filter.CustomUnit, StartTime: filter.StartTime,
		EndTime: filter.EndTime, EndExclusive: filter.EndExclusive,
		Limit: filter.Limit, Page: filter.Page, PageSize: filter.PageSize,
		CursorMode: filter.CursorMode, Model: filter.Model, AuthIndex: filter.AuthIndex,
		AuthType: filter.AuthType, APIGroupKey: key, Result: filter.Result,
	}, err
}

func liveRecords(rows []repodto.UsageEventRecord) []servicedto.UsageEventRecord {
	result := make([]servicedto.UsageEventRecord, 0, len(rows))
	for _, row := range rows {
		result = append(result, servicedto.UsageEventRecord(row))
	}
	return result
}

func (s *usageService) LiveSnapshot(ctx context.Context, filter servicedto.UsageFilter) (*servicedto.UsageEventsPage, int64, error) {
	query, err := s.liveFilter(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	page, high, err := repository.UsageEventsLiveSnapshot(s.db.WithContext(ctx), query, s.pricing.Snapshot())
	if err != nil {
		return nil, 0, err
	}
	return &servicedto.UsageEventsPage{Events: liveRecords(page.Events), TotalCount: page.TotalCount, Page: page.Page, PageSize: page.PageSize, TotalPages: page.TotalPages, HasMore: page.HasMore}, high, nil
}

func (s *usageService) LiveCount(ctx context.Context, filter servicedto.UsageFilter) (int64, error) {
	query, err := s.liveFilter(ctx, filter)
	if err != nil {
		return 0, err
	}
	return repository.UsageEventsLiveCount(s.db.WithContext(ctx), query)
}

func (s *usageService) LiveHighID(ctx context.Context) (int64, error) {
	return repository.UsageEventsLiveHighID(s.db.WithContext(ctx))
}

func (s *usageService) LiveIDBatch(ctx context.Context, filter servicedto.UsageFilter, after int64, limit int) ([]servicedto.UsageEventRecord, int64, int, error) {
	query, err := s.liveFilter(ctx, filter)
	if err != nil {
		return nil, after, 0, err
	}
	rows, high, scanned, err := repository.UsageEventsLiveIDBatch(s.db.WithContext(ctx), query, after, limit, s.pricing.Snapshot())
	return liveRecords(rows), high, scanned, err
}

func (s *usageService) LiveTimeBatch(ctx context.Context, filter servicedto.UsageFilter, high int64, from, until time.Time, lastTime *time.Time, lastID int64, limit int) ([]servicedto.UsageEventRecord, *time.Time, int64, int, error) {
	query, err := s.liveFilter(ctx, filter)
	if err != nil {
		return nil, lastTime, lastID, 0, err
	}
	rows, nextTime, nextID, scanned, err := repository.UsageEventsLiveTimeBatch(s.db.WithContext(ctx), query, high, from, until, lastTime, lastID, limit, s.pricing.Snapshot())
	return liveRecords(rows), nextTime, nextID, scanned, err
}
