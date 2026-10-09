package test

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	. "cpa-usage-keeper/internal/api"
)

func TestUsageOverviewAcceptsCustomDayRangeOlderThanThirtyDays(t *testing.T) {
	now := time.Now().In(time.Local)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	startDay := today.AddDate(0, 0, -120)
	expectedEnd := today.AddDate(0, 0, 1)
	query := url.Values{"range": {"custom"}, "unit": {"day"}, "start": {startDay.Format(time.DateOnly)}, "end": {today.Format(time.DateOnly)}}
	for _, viewer := range []bool{false, true} {
		path, keyID := "/api/v1/usage/overview", ""
		provider := &usageEventsStub{}
		var router http.Handler = NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
		var cookies []*http.Cookie
		if viewer {
			var cookie *http.Cookie
			router, cookie = newUsageViewerRouter(t, provider)
			cookies = []*http.Cookie{cookie}
			path, keyID = "/api/v1/key-overview", "42"
		}
		t.Run(path, func(t *testing.T) {
			response := serveAPIGet(router, path+"?"+query.Encode(), cookies...)
			if response.Code != http.StatusOK {
				t.Fatalf("long custom overview status=%d body=%s", response.Code, response.Body.String())
			}
			filter := provider.lastFilter
			if provider.overviewCalls != 1 || filter.APIKeyID != keyID {
				t.Fatalf("expected one overview query for key %q, got calls=%d filter=%+v", keyID, provider.overviewCalls, filter)
			}
			if filter.StartTime == nil || !filter.StartTime.Equal(startDay) || filter.EndTime == nil || !filter.EndTime.Equal(expectedEnd) || !filter.EndExclusive {
				t.Fatalf("expected %s..%s exclusive, got %+v", startDay, expectedEnd, filter)
			}
			if filter.CustomUnit != "day" || filter.RangeCount != 121 {
				t.Fatalf("expected 121 complete custom day buckets, got %+v", filter)
			}
		})
	}
}
