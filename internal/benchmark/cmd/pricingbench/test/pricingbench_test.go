package test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// TestPricingBenchCLIExercisesRealOldSchemas 验证专用文件确实从旧物理结构升级，且冷热/历史五维场景均跑完重算与追赶。
func TestPricingBenchCLIExercisesRealOldSchemas(t *testing.T) {
	// 正式压测工具依赖 Linux；通用迁移与恢复用例仍由三平台 CI 执行。
	if runtime.GOOS != "linux" {
		t.Skip("formal pricing benchmark CLI requires Linux")
	}
	for _, scenario := range []string{"latest", "five-dim"} {
		t.Run(scenario, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), scenario)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			anchor := time.Now().UTC().Truncate(time.Hour).Format(time.RFC3339)
			command := exec.CommandContext(ctx, "go", "run", "-tags", "sqlite_trace", "../", "--root", root,
				"--events", "120", "--scenario", scenario, "--anchor", anchor, "--receive-interval", "5ms")
			command.Env = append(os.Environ(), "TZ=UTC", "GIN_MODE=release")
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("pricingbench %s failed: %v (context=%v)\n%s", scenario, err, ctx.Err(), output)
			}
			for _, phase := range []string{"old physical schema ready", "M1-M4", "M5-M6", "30-day recalculation", "process retained inbox once", "completed"} {
				if !strings.Contains(string(output), phase) {
					t.Fatalf("pricingbench %s omitted phase %q: %s", scenario, phase, output)
				}
			}
			encoded, err := os.ReadFile(filepath.Join(root, "pricing-report.json"))
			if err != nil {
				t.Fatal(err)
			}
			var report struct {
				HotEvents         int64 `json:"hot_events"`
				ArchiveEvents     int64 `json:"archive_events"`
				Recent30DayEvents int64 `json:"recent_30_day_events"`
				Rules             int64 `json:"rules"`
				TotalEvents       int64 `json:"total_events"`
				MigrationInbox    struct {
					Offered              int64   `json:"offered"`
					Committed            int64   `json:"committed"`
					BufferDropped        int64   `json:"buffer_dropped"`
					Errors               int64   `json:"errors"`
					VerifiedRows         int64   `json:"verified_rows"`
					P95OfferedToCommitMS float64 `json:"p95_offered_to_commit_ms"`
					P95CommitMS          float64 `json:"p95_commit_ms"`
				} `json:"migration_inbox"`
				RecalculationInbox struct {
					Offered       int64 `json:"offered"`
					Committed     int64 `json:"committed"`
					BufferDropped int64 `json:"buffer_dropped"`
					Errors        int64 `json:"errors"`
					VerifiedRows  int64 `json:"verified_rows"`
				} `json:"recalculation_inbox"`
				Recalculation struct {
					Status        string `json:"status"`
					TargetRows    int64  `json:"target_rows"`
					ProcessedRows int64  `json:"processed_rows"`
				} `json:"recalculation"`
				Catchup struct {
					ProcessedEvents int64 `json:"processed_events"`
				} `json:"catchup"`
				Queries struct {
					OverviewURL  string `json:"overview_url"`
					AnalysisURL  string `json:"analysis_url"`
					BeforePhase  string `json:"before_phase"`
					AfterPhase   string `json:"after_phase"`
					DuringErrors int64  `json:"during_errors"`
				} `json:"queries"`
				PreUpgradeTotalTokens  int64 `json:"pre_upgrade_total_tokens"`
				PostUpgradeTotalTokens int64 `json:"post_upgrade_total_tokens"`
				PostRecalcTotalTokens  int64 `json:"post_recalc_total_tokens"`
				WriterTrace            struct {
					Enabled               bool  `json:"enabled"`
					WriteTransactionCount int64 `json:"write_transaction_count"`
				} `json:"writer_trace"`
				Error string `json:"error"`
			}
			if err := json.Unmarshal(encoded, &report); err != nil {
				t.Fatal(err)
			}
			if report.Error != "" || report.TotalEvents != 120 || report.HotEvents+report.ArchiveEvents != 120 || report.Rules == 0 || report.Recent30DayEvents == 0 {
				t.Fatalf("bad generated workload: %+v", report)
			}
			if report.MigrationInbox.Offered == 0 || report.MigrationInbox.Offered != report.MigrationInbox.Committed || report.MigrationInbox.VerifiedRows != report.MigrationInbox.Committed || report.MigrationInbox.BufferDropped != 0 || report.MigrationInbox.Errors != 0 || report.MigrationInbox.P95OfferedToCommitMS < report.MigrationInbox.P95CommitMS {
				t.Fatalf("durable input accounting changed: %+v", report.MigrationInbox)
			}
			if report.RecalculationInbox.Offered == 0 || report.RecalculationInbox.Offered != report.RecalculationInbox.Committed || report.RecalculationInbox.VerifiedRows != report.RecalculationInbox.Committed || report.RecalculationInbox.BufferDropped != 0 || report.RecalculationInbox.Errors != 0 {
				t.Fatalf("recalculation durable input accounting changed: %+v", report.RecalculationInbox)
			}
			if report.Queries.BeforePhase != "post_upgrade_before_recalculation" || report.Queries.AfterPhase != "post_recalculation_and_inbox_catchup" || report.Queries.DuringErrors != 0 || !strings.Contains(report.Queries.OverviewURL, "range=custom") || !strings.Contains(report.Queries.AnalysisURL, "range=custom") {
				t.Fatalf("real 120-day query provenance missing: %+v", report.Queries)
			}
			if report.Recalculation.Status != "completed" || report.Recalculation.TargetRows != report.Recalculation.ProcessedRows || report.Recalculation.TargetRows == 0 || report.Catchup.ProcessedEvents == 0 {
				t.Fatalf("recalculation/catchup/gate did not complete: %+v", report)
			}
			if report.PreUpgradeTotalTokens == 0 || report.PreUpgradeTotalTokens != report.PostUpgradeTotalTokens || report.PostUpgradeTotalTokens != report.PostRecalcTotalTokens || !report.WriterTrace.Enabled || report.WriterTrace.WriteTransactionCount == 0 {
				t.Fatalf("pricing facts or trace missing: %+v", report)
			}
			repeated := exec.CommandContext(ctx, "go", "run", "-tags", "sqlite_trace", "../", "--root", root,
				"--events", "120", "--scenario", scenario, "--anchor", anchor)
			repeated.Env = command.Env
			if output, err := repeated.CombinedOutput(); err == nil || !strings.Contains(string(output), "already exists") {
				t.Fatalf("reused output directory was not rejected: err=%v output=%s", err, output)
			}
			if after, err := os.ReadFile(filepath.Join(root, "pricing-report.json")); err != nil || string(after) != string(encoded) {
				t.Fatalf("reused run changed the completed report: %v", err)
			}
			backupPath := onePricingM1Backup(t, root)
			backupDB := openPricingBenchTestDB(t, backupPath)
			assertPricingBenchColumn(t, backupDB, "usage_events", "cost_usd", false)
			assertPricingBenchColumn(t, backupDB, "model_price_settings", "branches_json", false)
			if scenario == "five-dim" {
				assertPricingBenchColumn(t, backupDB, "usage_overview_daily_stats", "service_tier", false)
				var indexCount int
				if err := backupDB.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='uniq_usage_overview_daily_stats_bucket_api_model_auth_alias'").Scan(&indexCount); err != nil || indexCount != 1 {
					t.Fatalf("old five-dimension unique index missing: %d, %v", indexCount, err)
				}
			} else if report.ArchiveEvents == 0 {
				t.Fatal("latest scenario omitted cold events")
			}
			liveDB := openPricingBenchTestDB(t, filepath.Join(root, "pricing-old.db"))
			assertPricingBenchColumn(t, liveDB, "usage_events", "cost_usd", true)
			var schemaComplete, dataComplete int
			if err := liveDB.QueryRow("SELECT schema_complete, data_complete FROM pricing_migration_state WHERE id=1").Scan(&schemaComplete, &dataComplete); err != nil || schemaComplete != 1 || dataComplete != 1 {
				t.Fatalf("pricing data did not reach M6: schema=%d data=%d err=%v", schemaComplete, dataComplete, err)
			}
		})
	}
}

func onePricingM1Backup(t *testing.T, root string) string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(filepath.Join(root, "backups"), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".db") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil || len(files) != 1 {
		t.Fatalf("expected one M1 backup, got %v: %v", files, err)
	}
	return files[0]
}

func openPricingBenchTestDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func assertPricingBenchColumn(t *testing.T, db *sql.DB, table, column string, want bool) {
	t.Helper()
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var index, notNull, primaryKey int
		var name, typ string
		var defaultValue sql.NullString
		if err := rows.Scan(&index, &name, &typ, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		found = found || name == column
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if found != want {
		t.Fatalf("%s.%s exists=%t want=%t", table, column, found, want)
	}
}
