package test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	keeperapi "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
)

const recalculationHTTPBody = `{"start_at":"2026-09-22T10:00:00+08:00","config_revision":8}`

func testRecalculationTask() servicedto.RecalculationTask {
	count := int64(42)
	return servicedto.RecalculationTask{
		TaskID: "task-1", Status: servicedto.RecalculationRunning, Stage: servicedto.RecalculationEvents,
		StartAt:        time.Date(2026, 9, 22, 10, 0, 0, 0, time.FixedZone("CST", 8*60*60)),
		EndAt:          time.Date(2026, 9, 23, 10, 30, 0, 0, time.FixedZone("CST", 8*60*60)),
		ConfigRevision: 8, ProcessedCount: 12, TotalCount: &count,
		UpdatedAt: time.Date(2026, 9, 23, 10, 31, 0, 0, time.FixedZone("CST", 8*60*60)),
	}
}

func TestPricingRecalculationsHTTPRoundTripsOptionsStartAndCurrent(t *testing.T) {
	task := testRecalculationTask()
	earliest := task.StartAt.Add(-24 * time.Hour)
	latest := task.StartAt
	stub := &pricingStub{
		recalcOptions: servicedto.RecalculationOptions{Timezone: "Asia/Shanghai", EarliestStart: &earliest, LatestStart: &latest, StepSeconds: 3600, MaxDays: 30, ConfigRevision: 8},
		recalcReply:   servicedto.StartRecalculationResponse{Started: true, Task: task}, recalcCurrent: &task,
	}
	router := keeperapi.NewRouter(nil, nil, nil, stub, keeperapi.AuthConfig{}, nil, "")
	options := httptest.NewRecorder()
	router.ServeHTTP(options, newPricingRequest(http.MethodGet, "/api/v1/pricing/recalculations/options", ""))
	var gotOptions servicedto.RecalculationOptions
	if options.Code != http.StatusOK || json.Unmarshal(options.Body.Bytes(), &gotOptions) != nil || gotOptions.Timezone != "Asia/Shanghai" || gotOptions.StepSeconds != 3600 || gotOptions.MaxDays != 30 || gotOptions.ConfigRevision != 8 {
		t.Fatalf("options status %d: %s", options.Code, options.Body.String())
	}
	started := httptest.NewRecorder()
	router.ServeHTTP(started, newPricingRequest(http.MethodPost, "/api/v1/pricing/recalculations", recalculationHTTPBody))
	var gotStart servicedto.StartRecalculationResponse
	if started.Code != http.StatusAccepted || json.Unmarshal(started.Body.Bytes(), &gotStart) != nil || !gotStart.Started || gotStart.Task.TaskID != task.TaskID || stub.recalcStart == nil || stub.recalcStart.ConfigRevision != 8 || !stub.recalcStart.StartAt.Equal(task.StartAt) {
		t.Fatalf("start status %d: %s input=%+v", started.Code, started.Body.String(), stub.recalcStart)
	}
	stub.recalcReply.Started = false
	repeated := httptest.NewRecorder()
	router.ServeHTTP(repeated, newPricingRequest(http.MethodPost, "/api/v1/pricing/recalculations", recalculationHTTPBody))
	if repeated.Code != http.StatusOK || json.Unmarshal(repeated.Body.Bytes(), &gotStart) != nil || gotStart.Started || gotStart.Task.TaskID != task.TaskID {
		t.Fatalf("running duplicate status %d: %s", repeated.Code, repeated.Body.String())
	}
	current := httptest.NewRecorder()
	router.ServeHTTP(current, newPricingRequest(http.MethodGet, "/api/v1/pricing/recalculations/current", ""))
	var gotCurrent servicedto.RecalculationTask
	if current.Code != http.StatusOK || json.Unmarshal(current.Body.Bytes(), &gotCurrent) != nil || gotCurrent.TaskID != task.TaskID || gotCurrent.ProcessedCount != 12 || gotCurrent.TotalCount == nil || *gotCurrent.TotalCount != 42 {
		t.Fatalf("current status %d: %s", current.Code, current.Body.String())
	}
	stub.recalcCurrent = nil
	empty := httptest.NewRecorder()
	router.ServeHTTP(empty, newPricingRequest(http.MethodGet, "/api/v1/pricing/recalculations/current", ""))
	if empty.Code != http.StatusOK || strings.TrimSpace(empty.Body.String()) != "null" {
		t.Fatalf("empty current status %d: %s", empty.Code, empty.Body.String())
	}
}

func TestPricingRecalculationsHTTPRequiresExactStartFields(t *testing.T) {
	stub := &pricingStub{recalcReply: servicedto.StartRecalculationResponse{Started: true, Task: testRecalculationTask()}}
	router := keeperapi.NewRouter(nil, nil, nil, stub, keeperapi.AuthConfig{}, nil, "")
	for _, tc := range []struct{ name, body, path string }{
		{"missing start", `{"config_revision":0}`, "start_at"},
		{"null start", `{"start_at":null,"config_revision":0}`, "start_at"},
		{"missing revision", `{"start_at":"2026-09-22T10:00:00+08:00"}`, "config_revision"},
		{"null revision", `{"start_at":"2026-09-22T10:00:00+08:00","config_revision":null}`, "config_revision"},
		{"unknown field", `{"start_at":"2026-09-22T10:00:00+08:00","config_revision":0,"request_id":"x"}`, ""},
		{"trailing JSON", recalculationHTTPBody + `{}`, ""},
		{"malformed JSON", `{"start_at":`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, newPricingRequest(http.MethodPost, "/api/v1/pricing/recalculations", tc.body))
			var got servicedto.PricingErrorResponse
			if response.Code != http.StatusBadRequest || json.Unmarshal(response.Body.Bytes(), &got) != nil || got.Code != "invalid_request" || stub.recalcStart != nil {
				t.Fatalf("invalid body status %d: %s called=%+v", response.Code, response.Body.String(), stub.recalcStart)
			}
			if tc.path != "" && (len(got.Fields) != 1 || got.Fields[0].Path != tc.path || got.Fields[0].Code != "required") {
				t.Fatalf("missing %s field path: %+v", tc.path, got.Fields)
			}
		})
	}
}

func TestPricingRecalculationsHTTPKeepsAdminBoundaryAndBypassesCostReadGate(t *testing.T) {
	sessions := auth.NewSessionManager(time.Hour)
	adminToken, _, err := sessions.Create()
	if err != nil {
		t.Fatal(err)
	}
	viewerToken, _, err := sessions.CreateAPIKeyViewer(42)
	if err != nil {
		t.Fatal(err)
	}
	gate := service.NewCostReadGate()
	resume, err := gate.BlockAndDrain(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer resume()
	config := keeperapi.AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}
	stub := &pricingStub{recalcReply: servicedto.StartRecalculationResponse{Started: true, Task: testRecalculationTask()}}
	router := keeperapi.NewRouter(nil, nil, nil, stub, config, keeperapi.NewAuthHandler(config, sessions), "", keeperapi.OptionalProviders{CostReadGate: gate})
	for _, route := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/pricing/recalculations/options", ""},
		{http.MethodGet, "/api/v1/pricing/recalculations/current", ""},
		{http.MethodPost, "/api/v1/pricing/recalculations", recalculationHTTPBody},
	} {
		anonymous := httptest.NewRecorder()
		router.ServeHTTP(anonymous, newPricingRequest(route.method, route.path, route.body))
		if anonymous.Code != http.StatusUnauthorized {
			t.Fatalf("anonymous %s status %d", route.path, anonymous.Code)
		}
		viewerRequest := newPricingRequest(route.method, route.path, route.body)
		viewerRequest.AddCookie(&http.Cookie{Name: standardSessionCookieName, Value: viewerToken})
		viewer := httptest.NewRecorder()
		router.ServeHTTP(viewer, viewerRequest)
		if viewer.Code != http.StatusForbidden {
			t.Fatalf("viewer %s status %d", route.path, viewer.Code)
		}
		adminRequest := newPricingRequest(route.method, route.path, route.body)
		adminRequest.AddCookie(&http.Cookie{Name: standardSessionCookieName, Value: adminToken})
		admin := httptest.NewRecorder()
		router.ServeHTTP(admin, adminRequest)
		if admin.Code == http.StatusServiceUnavailable || admin.Code >= 400 {
			t.Fatalf("admin %s blocked by cost gate: %d %s", route.path, admin.Code, admin.Body.String())
		}
	}
}

func TestPricingRecalculationsHTTPMapsBusinessErrorsAndSanitizesInternalFailures(t *testing.T) {
	for _, tc := range []struct {
		name       string
		err        error
		status     int
		code, path string
	}{
		{"busy", service.ErrPricingBusy, http.StatusConflict, "pricing_busy", ""},
		{"changed", service.ErrPricingChanged, http.StatusConflict, "pricing_changed", "config_revision"},
		{"invalid start", fmt.Errorf("%w: %w", service.ErrInvalidPricingRecalculationStart, &pricing.ValidationError{Path: "start_at", Code: "invalid", Reason: "secret internal reason"}), http.StatusBadRequest, "invalid_request", "start_at"},
		{"internal", errors.New("SELECT secret FROM usage_events: disk failed"), http.StatusInternalServerError, "internal_error", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := &pricingStub{recalcErr: tc.err, err: tc.err}
			router := keeperapi.NewRouter(nil, nil, nil, stub, keeperapi.AuthConfig{}, nil, "")
			for _, route := range []struct{ method, path, body string }{
				{http.MethodGet, "/api/v1/pricing/recalculations/options", ""},
				{http.MethodGet, "/api/v1/pricing/recalculations/current", ""},
				{http.MethodPost, "/api/v1/pricing/recalculations", recalculationHTTPBody},
			} {
				response := httptest.NewRecorder()
				router.ServeHTTP(response, newPricingRequest(route.method, route.path, route.body))
				var got servicedto.PricingErrorResponse
				if response.Code != tc.status || json.Unmarshal(response.Body.Bytes(), &got) != nil || got.Code != tc.code || strings.Contains(response.Body.String(), "secret") {
					t.Fatalf("%s %s status %d: %s", tc.name, route.path, response.Code, response.Body.String())
				}
				if tc.path != "" && (len(got.Fields) != 1 || got.Fields[0].Path != tc.path) {
					t.Fatalf("%s missing %s field: %+v", tc.name, tc.path, got.Fields)
				}
			}
			if tc.name == "busy" {
				response := httptest.NewRecorder()
				router.ServeHTTP(response, newPricingRequest(http.MethodPut, "/api/v1/pricing/models", completePricingHTTPBody))
				if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"code":"pricing_busy"`) {
					t.Fatalf("complete configuration write did not map busy: %d %s", response.Code, response.Body.String())
				}
			}
		})
	}
}
