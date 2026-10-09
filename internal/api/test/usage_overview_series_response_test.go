package test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	. "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/entities"
	repositorydto "cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/service"
)

type overviewSeriesJSON struct {
	Buckets       []string   `json:"buckets"`
	Requests      []int64    `json:"requests"`
	Tokens        []int64    `json:"tokens"`
	RPM           []float64  `json:"rpm"`
	TPM           []float64  `json:"tpm"`
	Cost          []float64  `json:"cost"`
	CacheReadRate []*float64 `json:"cache_read_rate"`
}

func TestLongCustomDayOverviewCapsAlignedSeriesAtNinetyPoints(t *testing.T) {
	db := openAPITestDatabase(t)

	now := time.Now().In(time.Local)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	start := today.AddDate(0, 0, -120)
	rows := make([]entities.UsageOverviewDailyStat, 0, 121)
	for bucket := start; !bucket.After(today); bucket = bucket.AddDate(0, 0, 1) {
		rows = append(rows, entities.UsageOverviewDailyStat{
			BucketStart: bucket, APIGroupKey: "provider-a", Model: "model-a",
			RequestCount: 1, SuccessCount: 1, InputTokens: 10, CacheReadTokens: 4, TotalTokens: 10,
		})
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("seed daily rollups: %v", err)
	}

	query := url.Values{
		"range": {"custom"}, "unit": {"day"},
		"start": {start.Format(time.DateOnly)}, "end": {today.Format(time.DateOnly)},
	}
	router := NewRouter(nil, nil, service.NewUsageService(db, emptyPricingCatalogForTest()), nil, AuthConfig{}, nil, "")
	response := serveAPIGet(router, "/api/v1/usage/overview?"+query.Encode())
	if response.Code != http.StatusOK {
		t.Fatalf("Overview status=%d body=%s", response.Code, response.Body.String())
	}

	var payload struct {
		Usage  repositorydto.StatisticsSnapshot `json:"usage"`
		Series overviewSeriesJSON               `json:"series"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode long Overview response: %v body=%s", err, response.Body.String())
	}
	if payload.Usage.TotalRequests != 121 || payload.Usage.TotalTokens != 1210 {
		t.Fatalf("top totals must use the complete range, got %+v", payload.Usage)
	}
	if len(payload.Series.Buckets) != 90 {
		t.Fatalf("expected exactly 90 merged points, got %d", len(payload.Series.Buckets))
	}
	for name, length := range map[string]int{
		"requests": len(payload.Series.Requests), "tokens": len(payload.Series.Tokens),
		"rpm": len(payload.Series.RPM), "tpm": len(payload.Series.TPM),
		"cost": len(payload.Series.Cost), "cache_read_rate": len(payload.Series.CacheReadRate),
	} {
		if length != len(payload.Series.Buckets) {
			t.Fatalf("%s length=%d, buckets=%d", name, length, len(payload.Series.Buckets))
		}
	}
	var seriesRequests int64
	for _, requests := range payload.Series.Requests {
		seriesRequests += requests
	}
	if seriesRequests != 121 {
		t.Fatalf("merged series lost requests: %d", seriesRequests)
	}
	for index, rate := range payload.Series.CacheReadRate {
		if rate == nil || *rate != 40 {
			t.Fatalf("merged cache rate[%d] = %v, want 40", index, rate)
		}
	}
}
