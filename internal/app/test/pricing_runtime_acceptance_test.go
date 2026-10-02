package test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	keeperapp "cpa-usage-keeper/internal/app"
	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
)

// TestPricingRuntimeHTTPUsesCompleteServices 通过真正监听的 App 验证完整启动与重算服务接线。
// 配置保存不改变旧费用；手动任务完成后明细与两个汇总一起变价，旧接口不再存在。
func TestPricingRuntimeHTTPUsesCompleteServices(t *testing.T) {
	cfg := pricingRuntimeConfig(t)
	application, baseURL, stop := startPricingRuntime(t, cfg)
	waitPricingRuntimePhase(t, baseURL, "ready")
	status, body := pricingRuntimeRequest(t, http.MethodPut, baseURL+"/api/v1/pricing/models", []byte(`{
		"model":"provider/model", "pricing_style":"openai",
		"base_prices":{"input":2,"output":0,"cache_read":0,"cache_write":0},
		"model_multiplier":1,"conditional_multipliers":[],"branches":[]
	}`))
	if status != http.StatusOK {
		t.Fatalf("save price: status=%d body=%s", status, body)
	}
	var saved servicedto.SavePricingModelResponse
	if err := json.Unmarshal(body, &saved); err != nil {
		t.Fatal(err)
	}
	cost, available := 7.0, true
	eventTime := time.Now().Add(-2 * time.Minute)
	if _, _, err := repository.InsertUsageEvents(application.DB, []entities.UsageEvent{{
		EventKey: "runtime-fixed-cost", Model: "provider/model", Timestamp: eventTime,
		InputTokens: 1_000_000, TotalTokens: 1_000_000, CostUSD: &cost, CostAvailable: &available,
	}}); err != nil {
		t.Fatal(err)
	}
	var before entities.UsageEvent
	if err := application.DB.First(&before).Error; err != nil || before.CostUSD == nil || *before.CostUSD != 7 {
		t.Fatalf("existing fee changed without recalculation: %+v err=%v", before, err)
	}
	request, err := json.Marshal(servicedto.StartRecalculationRequest{
		StartAt: eventTime.Truncate(time.Hour), ConfigRevision: saved.ConfigRevision,
	})
	if err != nil {
		t.Fatal(err)
	}
	status, body = pricingRuntimeRequest(t, http.MethodPost, baseURL+"/api/v1/pricing/recalculations", request)
	if status != http.StatusAccepted {
		t.Fatalf("start real recalculation: status=%d body=%s", status, body)
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		status, body = pricingRuntimeRequest(t, http.MethodGet, baseURL+"/api/v1/pricing/recalculations/current", nil)
		var task servicedto.RecalculationTask
		if status != http.StatusOK || json.Unmarshal(body, &task) != nil {
			t.Fatalf("current task: status=%d body=%s", status, body)
		}
		if task.Status == "failed" {
			t.Fatalf("real recalculation failed: %+v", task.Error)
		}
		if task.Status == "completed" {
			if task.ProcessedCount != 1 || task.TotalCount == nil || *task.TotalCount != 1 {
				t.Fatalf("unexpected committed progress: %+v", task)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("recalculation did not finish: %s", body)
		}
		time.Sleep(20 * time.Millisecond)
	}
	for _, table := range []string{"usage_events", "usage_overview_hourly_stats", "usage_overview_daily_stats"} {
		var total float64
		if err := application.DB.Table(table).Select("SUM(cost_usd)").Scan(&total).Error; err != nil {
			t.Fatal(err)
		}
		if !(math.Abs(total-2) <= 1e-12) {
			t.Fatalf("%s stored total=%v, want 2", table, total)
		}
	}
	status, _ = pricingRuntimeRequest(t, http.MethodGet, baseURL+"/api/v1/pricing", nil)
	if status != http.StatusNotFound {
		t.Fatalf("legacy pricing endpoint status=%d, want 404", status)
	}

	// 停止 Keeper 时，等待在途读取的重算必须随 App 生命周期退出，不能卡住后台停止。
	readDone, ok := application.CostReadGate.Acquire()
	if !ok {
		t.Fatal("completed task did not release cost reads")
	}
	t.Cleanup(readDone)
	status, body = pricingRuntimeRequest(t, http.MethodPost, baseURL+"/api/v1/pricing/recalculations", request)
	if status != http.StatusAccepted {
		t.Fatalf("second start: %d %s", status, body)
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		release, allowed := application.CostReadGate.Acquire()
		if !allowed {
			break
		}
		release()
		if time.Now().After(deadline) {
			t.Fatal("task did not reach read draining")
		}
		time.Sleep(10 * time.Millisecond)
	}
	stop()
	pricingProvider, ok := application.PricingService.(service.PricingProvider)
	if !ok {
		t.Fatal("application did not wire the complete pricing service")
	}
	for {
		task, err := pricingProvider.CurrentPricingRecalculation(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if task != nil && task.Status != servicedto.RecalculationRunning {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("task did not exit with application lifecycle")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestPricingRuntimeFailureKeepsPublicShell 证明无法打开数据库时仍可看失败状态，普通 API 不误报就绪。
func TestPricingRuntimeFailureKeepsPublicShell(t *testing.T) {
	cfg := pricingRuntimeConfig(t)
	cfg.SQLitePath = t.TempDir() // 目录不是 SQLite 文件，产生真实初始化错误。
	_, baseURL, _ := startPricingRuntime(t, cfg)
	waitPricingRuntimePhase(t, baseURL, "failed")
	status, body := pricingRuntimeRequest(t, http.MethodGet, baseURL+"/healthz", nil)
	if status != http.StatusOK {
		t.Fatalf("failed startup health: %d %s", status, body)
	}
	status, body = pricingRuntimeRequest(t, http.MethodGet, baseURL+"/api/v1/pricing/models", nil)
	if status != http.StatusServiceUnavailable || !bytes.Contains(body, []byte("migration_in_progress")) {
		t.Fatalf("failed startup exposed business: %d %s", status, body)
	}
}

func pricingRuntimeConfig(t *testing.T) config.Config {
	t.Helper()
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{}`)
	}))
	t.Cleanup(cpa.Close)
	cfg := testAppConfig(t)
	cfg.AppHost, cfg.AppBasePath = "127.0.0.1", "/keeper"
	cfg.CPABaseURL, cfg.CPAManagementKey = cpa.URL, "test-only"
	cfg.RequestTimeout, cfg.MetadataSyncInterval = 200*time.Millisecond, time.Hour
	cfg.BackupEnabled = false
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cfg.AppPort = fmt.Sprint(listener.Addr().(*net.TCPAddr).Port)
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func startPricingRuntime(t *testing.T, cfg config.Config) (*keeperapp.App, string, context.CancelFunc) {
	t.Helper()
	application, err := keeperapp.NewWithConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- application.RunContext(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("stop runtime: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Error("runtime did not stop after cancellation")
		}
		if err := application.Close(); err != nil {
			t.Errorf("close runtime: %v", err)
		}
	})
	return application, "http://" + cfg.ListenAddress() + cfg.AppBasePath, cancel
}

func waitPricingRuntimePhase(t *testing.T, baseURL, phase string) {
	t.Helper()
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		response, err := client.Get(baseURL + "/api/v1/startup/status")
		if err == nil {
			var state struct {
				Phase string `json:"phase"`
			}
			decodeErr := json.NewDecoder(response.Body).Decode(&state)
			_ = response.Body.Close()
			if decodeErr == nil && state.Phase == phase {
				return
			}
			if decodeErr == nil && state.Phase == "failed" && phase != "failed" {
				t.Fatal("runtime initialization failed")
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("runtime did not reach phase %s", phase)
}

func pricingRuntimeRequest(t *testing.T, method, target string, body []byte) (int, []byte) {
	t.Helper()
	request, err := http.NewRequest(method, target, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-CPA-Usage-Keeper-Request", "fetch")
	request.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Timeout: 3 * time.Second}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, data
}
