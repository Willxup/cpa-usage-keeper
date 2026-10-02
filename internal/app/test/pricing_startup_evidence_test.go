package test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"io"
	"math"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	keeperapp "cpa-usage-keeper/internal/app"
	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
	webui "cpa-usage-keeper/web"
	"gorm.io/gorm"
)

type pricingEvidenceRuntime struct {
	app    *keeperapp.App
	cancel context.CancelFunc
	done   chan error
	once   sync.Once
}

// startPricingEvidenceRuntime 让测试走真实监听与初始化，并允许先完全关库再重启同一文件。
func startPricingEvidenceRuntime(t *testing.T, cfg config.Config) *pricingEvidenceRuntime {
	t.Helper()
	application, err := keeperapp.NewWithConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	runtime := &pricingEvidenceRuntime{app: application, cancel: cancel, done: make(chan error, 1)}
	go func() { runtime.done <- application.RunContext(ctx) }()
	t.Cleanup(func() { runtime.stop(t) })
	return runtime
}

// stop 先结束HTTP和接收生命周期，等待RunContext退出后再关唯一writer与reader池。
func (runtime *pricingEvidenceRuntime) stop(t *testing.T) {
	t.Helper()
	runtime.once.Do(func() {
		runtime.cancel()
		select {
		case err := <-runtime.done:
			if err != nil {
				t.Errorf("stop pricing runtime: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Error("pricing runtime did not stop after cancellation")
		}
		if err := runtime.app.Close(); err != nil {
			t.Errorf("close pricing runtime: %v", err)
		}
	})
}

// waitPricingEvidencePhase 经真实HTTP连接读取公开状态；ready等待中出现failed立即报告。
func waitPricingEvidencePhase(t *testing.T, client *http.Client, baseURL, phase string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		response, err := client.Get(baseURL + "/api/v1/startup/status")
		if err == nil {
			var status struct {
				Phase string `json:"phase"`
			}
			decodeErr := json.NewDecoder(response.Body).Decode(&status)
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK && decodeErr == nil && status.Phase == phase {
				return
			}
			if decodeErr == nil && status.Phase == "failed" && phase != "failed" {
				t.Fatal("pricing runtime failed before ready")
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("pricing runtime did not reach phase %s", phase)
}

// TestPricingStartupTLSHandshakeServesBasePathAndReady 验证生产RunContext的TLS分支可完成握手并切换业务路由。
func TestPricingStartupTLSHandshakeServesBasePathAndReady(t *testing.T) {
	// 使用独立页面验证真实 TLS 与路由切换，不依赖开发目录里的前端构建产物。
	// 本测试不并行；恢复 Static 的清理先注册，确保运行时完全停止后才恢复。
	const page = "<html><body>pricing TLS fixture</body></html>"
	previousStatic := webui.Static
	webui.Static = fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte(page)}}
	t.Cleanup(func() { webui.Static = previousStatic })
	cfg := pricingRuntimeConfig(t)
	certificate, key := pricingEvidenceCertificate(t)
	cfg.TLSEnabled = true
	cfg.TLSCertFile, cfg.TLSKeyFile = filepath.Join(t.TempDir(), "cert.pem"), filepath.Join(t.TempDir(), "key.pem")
	if err := os.WriteFile(cfg.TLSCertFile, certificate, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.TLSKeyFile, key, 0600); err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certificate) {
		t.Fatal("test TLS certificate was not trusted")
	}
	client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots}}}
	t.Cleanup(client.CloseIdleConnections)
	_ = startPricingEvidenceRuntime(t, cfg)
	baseURL := "https://" + cfg.ListenAddress() + cfg.AppBasePath
	waitPricingEvidencePhase(t, client, baseURL, "ready")
	for _, path := range []string{"/", "/healthz"} {
		response, err := client.Get(baseURL + path)
		if err != nil {
			t.Fatalf("TLS request %s: %v", path, err)
		}
		body, readErr := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if readErr != nil || response.StatusCode != http.StatusOK || (path == "/" && !strings.Contains(string(body), "pricing TLS fixture")) {
			t.Fatalf("TLS path %s: status=%d body=%q error=%v", path, response.StatusCode, body, readErr)
		}
	}
	outside, err := client.Get("https://" + cfg.ListenAddress() + "/api/v1/startup/status")
	if err != nil {
		t.Fatal(err)
	}
	_ = outside.Body.Close()
	if outside.StatusCode == http.StatusOK {
		t.Fatal("startup status escaped configured base path")
	}
}

// pricingEvidenceCertificate 只为本地TLS验收签发127.0.0.1证书，不依赖主机信任库或生产密钥。
func pricingEvidenceCertificate(t *testing.T) ([]byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "pricing-startup-test"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:    x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	privateKey, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKey})
}

// TestPricingFailedOverviewMigrationKeepsShellAndResumesOnRestart 验证M4完成后的M5故障仍可访问外壳，重启复用唯一备份。
func TestPricingFailedOverviewMigrationKeepsShellAndResumesOnRestart(t *testing.T) {
	cfg := pricingRuntimeConfig(t)
	seed := seedPublishedPricingRuntime(t, cfg)
	state, err := repository.BootstrapPricingInitialization(context.Background(), seed)
	if err != nil || state.InitKind != repository.PricingInitKindLegacy {
		t.Fatalf("seed legacy pricing identity: %+v, %v", state, err)
	}
	if err := seed.Exec(`CREATE TRIGGER fail_pricing_overview_start
		BEFORE UPDATE OF phase ON pricing_migration_state
		WHEN NEW.phase = 'overview_backfilling'
		BEGIN SELECT RAISE(ABORT, 'test overview transition'); END`).Error; err != nil {
		t.Fatal(err)
	}
	closePricingStartupPools(t, seed, seed)

	first := startPricingEvidenceRuntime(t, cfg)
	baseURL := "http://" + cfg.ListenAddress() + cfg.AppBasePath
	client := &http.Client{Timeout: 3 * time.Second}
	waitPricingEvidencePhase(t, client, baseURL, "failed")
	for _, path := range []string{"/healthz", "/api/v1/pricing/models"} {
		response, err := client.Get(baseURL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, readErr := io.ReadAll(response.Body)
		_ = response.Body.Close()
		want := http.StatusOK
		if path != "/healthz" {
			want = http.StatusServiceUnavailable
		}
		if readErr != nil || response.StatusCode != want || (want == http.StatusServiceUnavailable && !strings.Contains(string(body), "migration_in_progress")) {
			t.Fatalf("failed startup path %s: status=%d body=%s error=%v", path, response.StatusCode, body, readErr)
		}
	}
	var interrupted entities.PricingMigrationState
	if err := first.app.DB.Where("id = ?", 1).Take(&interrupted).Error; err != nil || !interrupted.SchemaComplete || interrupted.DataComplete || interrupted.Phase != "events_backfilled" || interrupted.BackupPath == nil {
		t.Fatalf("M5 failure lost schema/data boundary or backup: %+v, %v", interrupted, err)
	}
	var backfilled struct {
		CostUSD       *float64
		CostAvailable *bool
	}
	if err := first.app.DB.Table("usage_events").Select("cost_usd, cost_available").Where("event_key = ?", "old-usage").Scan(&backfilled).Error; err != nil || backfilled.CostUSD == nil || backfilled.CostAvailable == nil || !*backfilled.CostAvailable || math.Abs(*backfilled.CostUSD-0.0002) > 1e-12 {
		t.Fatalf("M4 event fee was not committed before M5 failure: %+v, %v", backfilled, err)
	}
	backupPath := *interrupted.BackupPath
	if _, err := os.Stat(backupPath); err != nil {
		t.Fatalf("protected original backup is unavailable: %v", err)
	}
	first.stop(t)

	repair, err := repository.OpenDatabaseConnection(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := repair.Exec("DROP TRIGGER fail_pricing_overview_start").Error; err != nil {
		t.Fatal(err)
	}
	closePricingStartupPools(t, repair, repair)
	restartedConfig := pricingRuntimeConfig(t)
	restartedConfig.SQLitePath, restartedConfig.BackupDir = cfg.SQLitePath, cfg.BackupDir
	second := startPricingEvidenceRuntime(t, restartedConfig)
	waitPricingEvidencePhase(t, client, "http://"+restartedConfig.ListenAddress()+restartedConfig.AppBasePath, "ready")
	var completed entities.PricingMigrationState
	if err := second.app.DB.Where("id = ?", 1).Take(&completed).Error; err != nil || !completed.SchemaComplete || !completed.DataComplete || completed.BackupPath == nil || *completed.BackupPath != backupPath {
		t.Fatalf("restart did not complete from original backup: %+v, %v", completed, err)
	}
	for _, table := range []string{"usage_events", "usage_overview_hourly_stats", "usage_overview_daily_stats"} {
		var fact struct {
			CostUSD      *float64
			InputTokens  int64
			TotalTokens  int64
			RequestCount int64
		}
		columns := "cost_usd, input_tokens, total_tokens"
		if table != "usage_events" {
			columns += ", request_count"
		}
		if err := second.app.DB.Table(table).Select(columns).Scan(&fact).Error; err != nil {
			t.Fatal(err)
		}
		if fact.CostUSD == nil || math.IsNaN(*fact.CostUSD) || math.Abs(*fact.CostUSD-0.0002) > 1e-12 || fact.InputTokens != 100 || fact.TotalTokens != 100 || (table != "usage_events" && fact.RequestCount != 1) {
			t.Fatalf("%s facts changed across failed startup and restart: %+v", table, fact)
		}
	}
	var checkpoint entities.UsageAggregationCheckpoint
	if err := second.app.DB.Where("name = ?", entities.UsageAggregationCheckpointOverview).Take(&checkpoint).Error; err != nil || checkpoint.LastAggregatedUsageEventID != 1 {
		t.Fatalf("overview checkpoint changed across restart: %+v, %v", checkpoint, err)
	}
}

// seedPublishedPricingRuntime 从一条已汇总请求恢复费用改版前已发布物理列，供普通及故障启动共用。
func seedPublishedPricingRuntime(t *testing.T, cfg config.Config) *gorm.DB {
	t.Helper()
	seed, err := repository.OpenDatabase(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.UpsertModelPriceSetting(seed, repodto.ModelPriceSettingInput{Model: "old-model", PromptPricePer1M: 2}); err != nil {
		t.Fatal(err)
	}
	zero, available := 0.0, true
	event := entities.UsageEvent{EventKey: "old-usage", APIGroupKey: "old-key", Model: "old-model", Timestamp: time.Date(2026, 9, 23, 10, 15, 0, 0, time.Local), InputTokens: 100, TotalTokens: 100, CostUSD: &zero, CostAvailable: &available}
	if _, _, err := repository.InsertUsageEvents(seed, []entities.UsageEvent{event}); err != nil {
		t.Fatal(err)
	}
	if err := repository.AggregateUsageOverviewStats(context.Background(), seed, time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"usage_events", "usage_events_archive"} {
		for _, column := range []string{"cost_usd", "cost_available"} {
			if err := seed.Exec("ALTER TABLE " + table + " DROP COLUMN " + column).Error; err != nil {
				t.Fatalf("restore old %s.%s: %v", table, column, err)
			}
		}
	}
	for _, table := range []string{"usage_overview_hourly_stats", "usage_overview_daily_stats"} {
		for _, column := range []string{"cost_usd", "unavailable_cost_count"} {
			if err := seed.Exec("ALTER TABLE " + table + " DROP COLUMN " + column).Error; err != nil {
				t.Fatalf("restore old %s.%s: %v", table, column, err)
			}
		}
	}
	for _, statement := range []string{
		"ALTER TABLE model_price_settings DROP COLUMN branches_json",
		"DROP TABLE pricing_state",
		"DROP TABLE pricing_migration_state",
		"DELETE FROM schema_migrations WHERE version = '20261002_pricing_storage_structure'",
	} {
		if err := seed.Exec(statement).Error; err != nil {
			t.Fatalf("restore old physical schema with %q: %v", statement, err)
		}
	}
	return seed
}
