package relayusage

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGLMAdapterParsesLimits(t *testing.T) {
	body := `{"code":200,"data":{"level":"plus","limits":[` +
		`{"type":"TOKENS_LIMIT","percentage":40,"usage":100000,"currentValue":40000,"remaining":60000,"nextResetTime":1750000000000,"unit":3,"number":5},` +
		`{"type":"TIME_LIMIT","percentage":10,"usage":0,"currentValue":0,"remaining":0,"nextResetTime":"","unit":0,"number":0}` +
		`]}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// GLM 直接把 token 放 Authorization，不带 Bearer。
		if got := r.Header.Get("Authorization"); got != "test-key" {
			t.Errorf("Authorization header = %q, want test-key", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	a := newGLMAdapter(server.Client())
	a.url = server.URL
	result, err := a.Fetch(context.Background(), "test-key", "")
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}
	if result.Platform != "glm" {
		t.Errorf("Platform = %q, want glm", result.Platform)
	}
	if len(result.Rows) != 2 {
		t.Fatalf("Rows count = %d, want 2", len(result.Rows))
	}
	first := result.Rows[0]
	if first.Key != "5hour_tokens" {
		t.Errorf("first Key = %q, want 5hour_tokens", first.Key)
	}
	if first.Used == nil || *first.Used != 40000 {
		t.Errorf("first Used = %v, want 40000", first.Used)
	}
	if first.Limit == nil || *first.Limit != 100000 {
		t.Errorf("first Limit = %v, want 100000", first.Limit)
	}
	if first.UsedPercent == nil || *first.UsedPercent != 40 {
		t.Errorf("first UsedPercent = %v, want 40", first.UsedPercent)
	}
	if first.ResetAt == "" {
		t.Errorf("first ResetAt should not be empty")
	}
}

func TestGLMAdapterRejectsNon200Code(t *testing.T) {
	body := `{"code":500,"data":{}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	a := newGLMAdapter(server.Client())
	a.url = server.URL
	if _, err := a.Fetch(context.Background(), "k", ""); err == nil {
		t.Fatal("expected error for non-200 code")
	}
}

func TestGLMAdapterPrefersPercentage(t *testing.T) {
	// percentage 与 currentValue/usage 不一致时，优先采用 GLM 直接返回的 percentage。
	body := `{"code":200,"data":{"level":"plus","limits":[` +
		`{"type":"TOKENS_LIMIT","percentage":70,"usage":100000,"currentValue":40000,"remaining":60000,"nextResetTime":1750000000000,"unit":3,"number":5}]}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	a := newGLMAdapter(server.Client())
	a.url = server.URL
	result, err := a.Fetch(context.Background(), "test-key", "")
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}
	first := result.Rows[0]
	// currentValue/usage = 40%，但 GLM 的 percentage = 70，应取 70。
	if first.UsedPercent == nil || *first.UsedPercent != 70 {
		t.Errorf("UsedPercent = %v, want 70 (from percentage, not currentValue/usage=40)", first.UsedPercent)
	}
}

func TestMiniMaxAdapterParsesWindows(t *testing.T) {
	body := `{"base_resp":{"status_code":0},"model_remains":[` +
		`{"model_name":"abab6.5s","start_time":1000000,"end_time":19000000,"remains_time":5000000,` +
		`"weekly_start_time":1000000,"weekly_end_time":169000000,"weekly_remains_time":84000000,` +
		`"current_interval_remaining_percent":70,"current_weekly_remaining_percent":50,` +
		`"current_interval_status":1,"current_weekly_status":1}]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q, want Bearer test-key", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	a := newMiniMaxAdapter(server.Client())
	a.cnURL = server.URL
	a.globalURL = server.URL
	result, err := a.Fetch(context.Background(), "test-key", "https://api.minimaxi.com/v1")
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}
	if len(result.Rows) != 2 {
		t.Fatalf("Rows count = %d, want 2 (interval + weekly)", len(result.Rows))
	}
	// interval: used_percent = 100 - 70 = 30
	interval := result.Rows[0]
	if interval.UsedPercent == nil || *interval.UsedPercent != 30 {
		t.Errorf("interval UsedPercent = %v, want 30", interval.UsedPercent)
	}
	if interval.Key != "interval_abab6.5s" {
		t.Errorf("interval Key = %q, want interval_abab6.5s", interval.Key)
	}
	// weekly: used_percent = 100 - 50 = 50
	weekly := result.Rows[1]
	if weekly.UsedPercent == nil || *weekly.UsedPercent != 50 {
		t.Errorf("weekly UsedPercent = %v, want 50", weekly.UsedPercent)
	}
}

func TestMiniMaxAdapterSkipsUnlimitedStatus(t *testing.T) {
	// status=3 表示不限量，对应窗口应被跳过。
	body := `{"base_resp":{"status_code":0},"model_remains":[` +
		`{"model_name":"m1","start_time":1000,"end_time":2000,"remains_time":500,` +
		`"weekly_start_time":1000,"weekly_end_time":2000,"weekly_remains_time":500,` +
		`"current_interval_remaining_percent":100,"current_weekly_remaining_percent":100,` +
		`"current_interval_status":3,"current_weekly_status":3}]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	a := newMiniMaxAdapter(server.Client())
	a.cnURL = server.URL
	a.globalURL = server.URL
	result, err := a.Fetch(context.Background(), "k", "")
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}
	if len(result.Rows) != 0 {
		t.Errorf("Rows count = %d, want 0 (unlimited skipped)", len(result.Rows))
	}
}

func TestKimiAdapterParsesUsageAndWindows(t *testing.T) {
	body := `{"usage":{"limit":"100000","used":"40000","remaining":"60000","resetTime":"2026-07-20T00:00:00Z"},` +
		`"limits":[{"window":{"duration":300,"timeUnit":"TIME_UNIT_MINUTE"},"detail":{"limit":"50000","used":"10000","remaining":"40000","resetTime":"2026-07-17T12:00:00Z"}}],` +
		`"parallel":{"limit":"10"}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q, want Bearer test-key", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	a := newKimiAdapter(server.Client())
	a.url = server.URL
	result, err := a.Fetch(context.Background(), "test-key", "")
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}
	// weekly_tokens + window + parallel = 3 rows
	if len(result.Rows) != 3 {
		t.Fatalf("Rows count = %d, want 3", len(result.Rows))
	}
	if result.Rows[0].Key != "weekly_tokens" {
		t.Errorf("first Key = %q, want weekly_tokens", result.Rows[0].Key)
	}
	if result.Rows[0].UsedPercent == nil || *result.Rows[0].UsedPercent != 40 {
		t.Errorf("weekly_tokens UsedPercent = %v, want 40", result.Rows[0].UsedPercent)
	}
	// parallel: used=0, limit=10
	parallel := result.Rows[2]
	if parallel.Key != "parallel_requests" {
		t.Errorf("third Key = %q, want parallel_requests", parallel.Key)
	}
	if parallel.Limit == nil || *parallel.Limit != 10 {
		t.Errorf("parallel Limit = %v, want 10", parallel.Limit)
	}
}

func TestKimiAdapterBearerPrefixTolerated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer already-bearer" {
			t.Errorf("Authorization = %q, want Bearer already-bearer", got)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"usage":{"limit":"1","used":"0","remaining":"1","resetTime":""}}`))
	}))
	defer server.Close()

	a := newKimiAdapter(server.Client())
	a.url = server.URL
	if _, err := a.Fetch(context.Background(), "Bearer already-bearer", ""); err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}
}

func TestDeepSeekAdapterParsesBalance(t *testing.T) {
	body := `{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"123.45","granted_balance":"50.00","topped_up_balance":"73.45"}]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q, want Bearer test-key", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	a := newDeepSeekAdapter(server.Client())
	a.url = server.URL
	result, err := a.Fetch(context.Background(), "test-key", "")
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}
	if result.Balance == nil {
		t.Fatal("Balance is nil")
	}
	if result.Balance.Available != 123.45 {
		t.Errorf("Available = %v, want 123.45", result.Balance.Available)
	}
	if result.Balance.Granted != 50 {
		t.Errorf("Granted = %v, want 50", result.Balance.Granted)
	}
	if result.Balance.ToppedUp != 73.45 {
		t.Errorf("ToppedUp = %v, want 73.45", result.Balance.ToppedUp)
	}
	if result.Balance.Currency != "CNY" {
		t.Errorf("Currency = %q, want CNY", result.Balance.Currency)
	}
	if len(result.Rows) != 0 {
		t.Errorf("Rows count = %d, want 0 (DeepSeek only has balance)", len(result.Rows))
	}
}

func TestDeepSeekAdapterEmptyBalanceError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"is_available":true,"balance_infos":[]}`))
	}))
	defer server.Close()

	a := newDeepSeekAdapter(server.Client())
	a.url = server.URL
	if _, err := a.Fetch(context.Background(), "k", ""); err == nil {
		t.Fatal("expected error for empty balance_infos")
	}
}
