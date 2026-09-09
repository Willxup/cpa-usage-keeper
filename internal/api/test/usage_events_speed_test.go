package test

import (
	"encoding/csv"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"testing"

	. "cpa-usage-keeper/internal/api"
	servicedto "cpa-usage-keeper/internal/service/dto"
)

func TestUsageEventSpeedWithoutTTFTAcrossListAndExports(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
		csv  bool
	}{
		{name: "request events", path: "/api/v1/usage/events?range=24h"},
		{name: "auth file history", path: "/api/v1/usage/events?cursor_mode=true&source=shared-auth&auth_type=1"},
		{name: "AI provider history", path: "/api/v1/usage/events?cursor_mode=true&source=shared-auth&auth_type=2"},
		{name: "JSON export", path: "/api/v1/usage/events/export?range=24h&format=json"},
		{name: "CSV export", path: "/api/v1/usage/events/export?range=24h&format=csv", csv: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := &usageEventsStub{events: []servicedto.UsageEventRecord{
				{ID: 1, LatencyMS: 2000, OutputTokens: 61, ReasoningTokens: 20},
				{ID: 2, LatencyMS: 0, OutputTokens: 61},
			}}
			router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if response.Code != http.StatusOK {
				t.Fatalf("expected status 200, got %d: %s", response.Code, response.Body.String())
			}

			var speed *float64
			var invalidSpeed *float64
			if tc.csv {
				records, err := csv.NewReader(response.Body).ReadAll()
				if err != nil || len(records) != 3 {
					t.Fatalf("expected CSV header and two events, got %v: %v", records, err)
				}
				column := slices.Index(records[0], "speed_tps")
				if column < 0 {
					t.Fatal("missing speed_tps CSV column")
				}
				value, err := strconv.ParseFloat(records[1][column], 64)
				if err != nil {
					t.Fatalf("expected a numeric speed without TTFT: %v", err)
				}
				speed = &value
				if records[2][column] != "" {
					t.Fatalf("expected empty speed for zero latency, got %q", records[2][column])
				}
			} else {
				var payload struct {
					Events []struct {
						SpeedTPS *float64 `json:"speed_tps"`
					} `json:"events"`
				}
				if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
					t.Fatalf("decode event speeds: %v", err)
				}
				if len(payload.Events) != 2 {
					t.Fatalf("expected two events, got %+v", payload)
				}
				speed, invalidSpeed = payload.Events[0].SpeedTPS, payload.Events[1].SpeedTPS
			}
			if speed == nil || math.Abs(*speed-30.5) > 0.000001 {
				t.Fatalf("expected 30.5 t/s without TTFT, got %v", speed)
			}
			if invalidSpeed != nil {
				t.Fatalf("expected no speed for zero latency, got %v", *invalidSpeed)
			}
		})
	}
}
