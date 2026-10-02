package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	keeperapi "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/poller"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/quota"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
	"cpa-usage-keeper/internal/timeutil"
	webui "cpa-usage-keeper/web"
	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

type pricingBenchOptions struct {
	Root            string
	Events          int64
	Scenario        string
	Seed            uint64
	Anchor          time.Time
	ReceiveInterval time.Duration
}

type pricingBenchReport struct {
	Version                   string                `json:"version"`
	Scenario                  string                `json:"scenario"`
	Anchor                    time.Time             `json:"anchor"`
	Seed                      uint64                `json:"seed"`
	TotalEvents               int64                 `json:"total_events"`
	HotEvents                 int64                 `json:"hot_events"`
	ArchiveEvents             int64                 `json:"archive_events"`
	Recent30DayEvents         int64                 `json:"recent_30_day_events"`
	Models                    int                   `json:"models"`
	Rules                     int                   `json:"rules"`
	GeneratorVersion          string                `json:"generator_version"`
	GeneratedFingerprint      string                `json:"generated_fingerprint"`
	Hardware                  pricingBenchHardware  `json:"hardware"`
	StartedAt                 time.Time             `json:"started_at"`
	FinishedAt                time.Time             `json:"finished_at"`
	PrepareSeconds            float64               `json:"prepare_seconds"`
	UpgradeM1M4Seconds        float64               `json:"upgrade_m1_m4_seconds"`
	UpgradeM5M6Seconds        float64               `json:"upgrade_m5_m6_seconds"`
	UpgradeReadySeconds       float64               `json:"upgrade_ready_seconds"`
	UpgradeEventsPerSecond    float64               `json:"upgrade_events_per_second"`
	ObservedPhasesSeconds     map[string]float64    `json:"observed_phases_seconds"`
	MigrationInbox            pricingBenchInbox     `json:"migration_inbox"`
	RecalculationInbox        pricingBenchInbox     `json:"recalculation_inbox"`
	MigrationResources        pricingBenchResources `json:"migration_resources"`
	RecalculationResources    pricingBenchResources `json:"recalculation_resources"`
	WriterTrace               WriterTraceSummary    `json:"writer_trace"`
	RecalculationTrace        WriterTraceSummary    `json:"recalculation_trace"`
	Recalculation             pricingBenchRecalc    `json:"recalculation"`
	Queries                   pricingBenchQueries   `json:"queries"`
	Catchup                   pricingBenchCatchup   `json:"catchup"`
	PostUpgradeCostNullRows   int64                 `json:"post_upgrade_cost_null_rows"`
	PostUpgradeOverviewCursor int64                 `json:"post_upgrade_overview_cursor"`
	PreUpgradeTotalTokens     int64                 `json:"pre_upgrade_total_tokens"`
	PostUpgradeTotalTokens    int64                 `json:"post_upgrade_total_tokens"`
	PostRecalcTotalTokens     int64                 `json:"post_recalc_total_tokens"`
	PostUpgradeEventCostUSD   float64               `json:"post_upgrade_event_cost_usd"`
	PostRecalcEventCostUSD    float64               `json:"post_recalc_event_cost_usd"`
	Error                     string                `json:"error,omitempty"`
}

type pricingBenchHardware struct {
	OS             string `json:"os"`
	Arch           string `json:"arch"`
	LogicalCPUs    int    `json:"logical_cpus"`
	MemoryBytes    uint64 `json:"memory_bytes"`
	DiskFreeBytes  uint64 `json:"disk_free_bytes_at_start"`
	DiskTotalBytes uint64 `json:"disk_total_bytes"`
}

type pricingBenchResources struct {
	SampleIntervalMS           int64   `json:"sample_interval_ms"`
	Samples                    int64   `json:"samples"`
	PeakRSSBytes               uint64  `json:"peak_rss_bytes"`
	PeakDatabaseBytes          int64   `json:"peak_database_bytes"`
	PeakWALBytes               int64   `json:"peak_wal_bytes"`
	PeakSQLiteTotalBytes       int64   `json:"peak_sqlite_total_bytes"`
	PeakBackupBytes            int64   `json:"peak_backup_bytes"`
	PeakTaskDatabaseBytes      int64   `json:"peak_task_database_and_backup_bytes"`
	PeakWriterConnectionsInUse int     `json:"peak_writer_connections_in_use"`
	MaxSampledInUseSpanMS      float64 `json:"max_sampled_writer_connection_in_use_span_ms"`
	WriterPoolWaitCountDelta   int64   `json:"writer_pool_wait_count_delta"`
	WriterPoolWaitMSDelta      float64 `json:"writer_pool_wait_ms_delta"`
}

type pricingBenchInbox struct {
	Mode                 string  `json:"mode"`
	TargetIntervalMS     float64 `json:"target_interval_ms"`
	BufferCapacity       int     `json:"buffer_capacity"`
	PeakBuffered         int     `json:"peak_buffered"`
	Offered              int64   `json:"offered"`
	BufferDropped        int64   `json:"buffer_dropped"`
	Committed            int64   `json:"committed"`
	Errors               int64   `json:"errors"`
	VerifiedRows         int64   `json:"verified_rows"`
	P50CommitMS          float64 `json:"p50_commit_ms"`
	P95CommitMS          float64 `json:"p95_commit_ms"`
	P99CommitMS          float64 `json:"p99_commit_ms"`
	MaxCommitMS          float64 `json:"max_commit_ms"`
	P95OfferedToCommitMS float64 `json:"p95_offered_to_commit_ms"`
	MaxOfferedToCommitMS float64 `json:"max_offered_to_commit_ms"`
	ElapsedSeconds       float64 `json:"elapsed_seconds"`
	CommittedPerSec      float64 `json:"committed_per_second"`
}

type pricingBenchRecalc struct {
	StartAt        time.Time `json:"start_at"`
	EndAt          time.Time `json:"end_at"`
	Status         string    `json:"status"`
	TargetRows     int64     `json:"target_rows"`
	ProcessedRows  int64     `json:"processed_rows"`
	WallSeconds    float64   `json:"wall_seconds"`
	RowsPerSecond  float64   `json:"rows_per_second"`
	UpdatedModel   string    `json:"updated_model"`
	ConfigRevision int64     `json:"config_revision"`
}

type pricingBenchQueries struct {
	OverviewURL      string  `json:"overview_url"`
	AnalysisURL      string  `json:"analysis_url"`
	BeforePhase      string  `json:"before_phase"`
	AfterPhase       string  `json:"after_phase"`
	BeforeOverviewMS float64 `json:"before_overview_ms"`
	BeforeAnalysisMS float64 `json:"before_analysis_ms"`
	AfterOverviewMS  float64 `json:"after_overview_ms"`
	AfterAnalysisMS  float64 `json:"after_analysis_ms"`
	DuringOK         int64   `json:"during_ok"`
	DuringCostsBusy  int64   `json:"during_costs_busy"`
	DuringErrors     int64   `json:"during_errors"`
}

type pricingBenchCatchup struct {
	ProcessedEvents int64   `json:"processed_events"`
	WallSeconds     float64 `json:"wall_seconds"`
	EventsPerSecond float64 `json:"events_per_second"`
}

// runPricingBenchmark 从专用合成旧文件开始，依次走现有 M0–M6、实际服务重算和 inbox 追赶。
func runPricingBenchmark(ctx context.Context, options pricingBenchOptions) (report pricingBenchReport, runErr error) {
	report = pricingBenchReport{
		Version: "pricing-bench-v1", Scenario: options.Scenario, Anchor: options.Anchor, Seed: options.Seed,
		TotalEvents: options.Events, StartedAt: time.Now(), ObservedPhasesSeconds: map[string]float64{},
	}
	defer func() { report.FinishedAt = time.Now() }()
	report.Hardware = pricingBenchmarkHardware(options.Root)
	path := filepath.Join(options.Root, "pricing-old.db")
	fmt.Printf("[pricingbench] generate %d synthetic events; scenario=%s\n", options.Events, options.Scenario)
	prepStarted := time.Now()
	spec, err := preparePricingDataset(ctx, path, options.Events, options.Scenario, options.Seed, options.Anchor)
	if err != nil {
		return report, err
	}
	report.PrepareSeconds = time.Since(prepStarted).Seconds()
	report.HotEvents, report.ArchiveEvents, report.Recent30DayEvents = spec.Hot, spec.Archive, spec.Recent30
	report.Models, report.Rules = spec.Models, spec.Rules
	report.PreUpgradeTotalTokens = spec.TotalTokens
	report.GeneratorVersion = spec.Generated.GeneratorVersion
	report.GeneratedFingerprint = spec.Generated.DimensionFingerprint
	fmt.Printf("[pricingbench] old physical schema ready; hot=%d archive=%d rules=%d\n", spec.Hot, spec.Archive, spec.Rules)
	// M1 额外保留一份备份，DDL/WAL 可能再接近一份旧库。用实际旧库大小做磁盘预检。
	free, _ := pricingBenchmarkDisk(options.Root)
	needed := uint64(2*pricingBenchFileSize(path)) + 2*1024*1024*1024
	if free > 0 && free < needed {
		return report, fmt.Errorf("insufficient disk for M1 backup and WAL: free=%d required=%d", free, needed)
	}
	config := config.Config{SQLitePath: path}
	writer, reader, err := repository.OpenUnmigratedDatabasePools(config)
	if err != nil {
		return report, err
	}
	defer closePricingBenchPools(reader, writer)
	writerSQL, err := writer.Clauses(dbresolver.Write).DB()
	if err != nil {
		return report, err
	}
	state, err := repository.BootstrapPricingInitialization(ctx, writer)
	if err != nil {
		return report, fmt.Errorf("bootstrap old pricing dataset: %w", err)
	}
	if state.InitKind != repository.PricingInitKindLegacy {
		return report, fmt.Errorf("old dataset did not bootstrap as legacy: state=%+v", state)
	}
	if err := repository.EnsurePricingBootstrapInbox(ctx, writer); err != nil {
		return report, err
	}
	stopTrace, err := startSQLiteWriterTrace(ctx, writer)
	if err != nil {
		return report, fmt.Errorf("start real writer trace: %w", err)
	}
	traceActive := true
	defer func() {
		if traceActive {
			_, _ = stopTrace(context.Background())
		}
	}()
	upgradeStart := time.Now()
	monitor := startPricingResourceMonitor(path, writerSQL, reader, upgradeStart, report.ObservedPhasesSeconds)
	receiver := startPricingInboxReceiver(ctx, writer, options.Anchor, options.ReceiveInterval, true)
	if err := receiver.waitFirst(ctx); err != nil {
		report.MigrationInbox = receiver.stop()
		report.MigrationResources = monitor.stop()
		return report, err
	}
	fmt.Println("[pricingbench] M1-M4: verified backup, published migrations, pricing columns, event backfill")
	migrateStart := time.Now()
	baseline, err := repository.MigrateLegacyPricingEvents(ctx, writer, reader, filepath.Join(options.Root, "backups"), options.Anchor)
	report.UpgradeM1M4Seconds = time.Since(migrateStart).Seconds()
	if err != nil {
		report.MigrationInbox = receiver.stop()
		report.MigrationResources = monitor.stop()
		return report, fmt.Errorf("M1-M4 migration: %w", err)
	}
	fmt.Println("[pricingbench] M5-M6: overview fees and full validation")
	completeStart := time.Now()
	err = repository.CompleteLegacyPricingData(ctx, writer, reader, baseline)
	report.UpgradeM5M6Seconds = time.Since(completeStart).Seconds()
	report.UpgradeReadySeconds = time.Since(upgradeStart).Seconds()
	if report.UpgradeReadySeconds > 0 {
		report.UpgradeEventsPerSecond = float64(spec.Events) / report.UpgradeReadySeconds
	}
	report.MigrationInbox = receiver.stop()
	report.MigrationResources = monitor.stop()
	if err != nil {
		return report, fmt.Errorf("M5-M6 migration: %w", err)
	}
	report.WriterTrace, err = stopTrace(ctx)
	traceActive = false
	if err != nil {
		return report, fmt.Errorf("stop real writer trace: %w", err)
	}
	if err := requireCompletePricingInbox(report.MigrationInbox); err != nil {
		return report, fmt.Errorf("synthetic arrivals during migration: %w", err)
	}
	if err := repository.VerifyPricingStartupDataComplete(ctx, writer); err != nil {
		return report, fmt.Errorf("verify pricing startup data: %w", err)
	}
	if err := repository.RunDatabaseMigrationsWithBackup(ctx, writer, reader, filepath.Join(options.Root, "backups")); err != nil {
		return report, fmt.Errorf("run remaining published migrations: %w", err)
	}
	if err := verifyPricingUpgradeCounts(ctx, reader, spec, &report); err != nil {
		return report, err
	}
	if err := verifyPricingBenchInbox(ctx, reader, "pricingbench:usage", report.MigrationInbox.Committed); err != nil {
		return report, fmt.Errorf("verify migration inbox: %w", err)
	}
	report.MigrationInbox.VerifiedRows = report.MigrationInbox.Committed
	fmt.Printf("[pricingbench] upgrade ready; M1-M4=%.3fs M5-M6=%.3fs inbox=%d\n", report.UpgradeM1M4Seconds, report.UpgradeM5M6Seconds, report.MigrationInbox.Committed)
	if err := runPricingRecalculationBenchmark(ctx, writer, reader, writerSQL, spec, options, &report); err != nil {
		return report, err
	}
	return report, nil
}

func closePricingBenchPools(reader, writer *gorm.DB) {
	if reader != nil && reader != writer {
		if pool, err := reader.DB(); err == nil {
			_ = pool.Close()
		}
	}
	if writer != nil {
		if pool, err := writer.DB(); err == nil {
			_ = pool.Close()
		}
	}
}

func pricingBenchmarkHardware(path string) pricingBenchHardware {
	result := pricingBenchHardware{OS: runtime.GOOS, Arch: runtime.GOARCH, LogicalCPUs: runtime.NumCPU()}
	if contents, err := os.ReadFile("/proc/meminfo"); err == nil {
		for _, line := range strings.Split(string(contents), "\n") {
			if strings.HasPrefix(line, "MemTotal:") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					kb, _ := strconv.ParseUint(fields[1], 10, 64)
					result.MemoryBytes = kb * 1024
				}
			}
		}
	}
	result.DiskFreeBytes, result.DiskTotalBytes = pricingBenchmarkDisk(path)
	return result
}

type pricingResourceMonitor struct {
	stopCh chan struct{}
	doneCh chan pricingBenchResources
}

// startPricingResourceMonitor 只观察文件、进程和唯一 writer 池；InUse 是连接占用采样，不冒称 SQLite 写锁。
func startPricingResourceMonitor(path string, writer *sql.DB, reader *gorm.DB, started time.Time, phases map[string]float64) *pricingResourceMonitor {
	monitor := &pricingResourceMonitor{stopCh: make(chan struct{}), doneCh: make(chan pricingBenchResources, 1)}
	go func() {
		const interval = 50 * time.Millisecond
		result := pricingBenchResources{SampleIntervalMS: int64(interval / time.Millisecond)}
		initial := writer.Stats()
		var inUseSince time.Time
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		sample := func() {
			now := time.Now()
			result.Samples++
			stats := writer.Stats()
			result.PeakWriterConnectionsInUse = max(result.PeakWriterConnectionsInUse, stats.InUse)
			if stats.InUse > 0 {
				if inUseSince.IsZero() {
					inUseSince = now
				}
				result.MaxSampledInUseSpanMS = max(result.MaxSampledInUseSpanMS, now.Sub(inUseSince).Seconds()*1000)
			} else {
				inUseSince = time.Time{}
			}
			result.PeakRSSBytes = max(result.PeakRSSBytes, pricingBenchRSSBytes())
			dbBytes, walBytes, shmBytes := pricingBenchFileSize(path), pricingBenchFileSize(path+"-wal"), pricingBenchFileSize(path+"-shm")
			backupBytes := pricingBenchDirectoryBytes(filepath.Join(filepath.Dir(path), "backups"))
			result.PeakDatabaseBytes = max(result.PeakDatabaseBytes, dbBytes)
			result.PeakWALBytes = max(result.PeakWALBytes, walBytes)
			result.PeakSQLiteTotalBytes = max(result.PeakSQLiteTotalBytes, dbBytes+walBytes+shmBytes)
			result.PeakBackupBytes = max(result.PeakBackupBytes, backupBytes)
			result.PeakTaskDatabaseBytes = max(result.PeakTaskDatabaseBytes, dbBytes+walBytes+shmBytes+backupBytes)
			var phase string
			if err := reader.Table("pricing_migration_state").Select("phase").Where("id = 1").Scan(&phase).Error; err == nil && phase != "" {
				if _, seen := phases[phase]; !seen {
					phases[phase] = now.Sub(started).Seconds()
				}
			}
		}
		sample()
		for {
			select {
			case <-ticker.C:
				sample()
			case <-monitor.stopCh:
				sample()
				final := writer.Stats()
				result.WriterPoolWaitCountDelta = final.WaitCount - initial.WaitCount
				result.WriterPoolWaitMSDelta = (final.WaitDuration - initial.WaitDuration).Seconds() * 1000
				monitor.doneCh <- result
				return
			}
		}
	}()
	return monitor
}

func (m *pricingResourceMonitor) stop() pricingBenchResources {
	close(m.stopCh)
	return <-m.doneCh
}

func pricingBenchFileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

func pricingBenchDirectoryBytes(path string) int64 {
	var total int64
	_ = filepath.WalkDir(path, func(name string, entry os.DirEntry, err error) error {
		if err == nil && !entry.IsDir() {
			if info, infoErr := entry.Info(); infoErr == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total
}

func pricingBenchRSSBytes() uint64 {
	contents, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(contents))
	if len(fields) < 2 {
		return 0
	}
	pages, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return 0
	}
	return pages * uint64(os.Getpagesize())
}

type pricingInboxReceiver struct {
	stopCh  chan struct{}
	doneCh  chan pricingBenchInbox
	firstCh chan error
	stopMu  sync.Once
	result  pricingBenchInbox
}

type pricingInboxOffer struct {
	raw       string
	offeredAt time.Time
}

// startPricingInboxReceiver 独立定时提供合成消息；有限缓冲暴露写锁积压，单worker走真实唯一writer。
func startPricingInboxReceiver(ctx context.Context, writer *gorm.DB, anchor time.Time, interval time.Duration, bootstrap bool) *pricingInboxReceiver {
	receiver := &pricingInboxReceiver{stopCh: make(chan struct{}), doneCh: make(chan pricingBenchInbox, 1), firstCh: make(chan error, 1)}
	const bufferCapacity = 4096
	offers := make(chan pricingInboxOffer, bufferCapacity)
	producerDone := make(chan struct {
		offered, dropped int64
		peak             int
	}, 1)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		defer close(offers)
		var stats struct {
			offered, dropped int64
			peak             int
		}
		defer func() { producerDone <- stats }()
		for sequence := int64(1); ; sequence++ {
			// 时间戳接近数据锚点，消息本身只存在于该测试进程和持久 inbox 中。
			raw := fmt.Sprintf(`{"timestamp":%q,"provider":"codex","api_key":"bench-key-001","model":"bench-model-001","request_id":"pricingbench-live-%d","tokens":{"input_tokens":100,"output_tokens":20}}`, anchor.Add(-10*time.Minute).Format(time.RFC3339Nano), sequence)
			stats.offered++
			select {
			case offers <- pricingInboxOffer{raw: raw, offeredAt: time.Now()}:
				stats.peak = max(stats.peak, len(offers))
			default:
				stats.dropped++
			}
			select {
			case <-receiver.stopCh:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	go func() {
		started := time.Now()
		var callLatencies, offeredLatencies []time.Duration
		result := pricingBenchInbox{Mode: "open_loop_buffered", TargetIntervalMS: interval.Seconds() * 1000, BufferCapacity: bufferCapacity}
		first := true
		for offer := range offers {
			callAt := time.Now()
			var err error
			if bootstrap {
				_, err = repository.InsertPricingBootstrapInboxRawMessages(ctx, writer, "pricingbench:usage", []string{offer.raw}, callAt)
			} else {
				_, err = repository.InsertRedisUsageInboxRawMessages(writer.WithContext(ctx), "pricingbench:usage", []string{offer.raw}, callAt)
			}
			if err != nil {
				result.Errors++
			} else {
				result.Committed++
				callLatencies = append(callLatencies, time.Since(callAt))
				offeredLatencies = append(offeredLatencies, time.Since(offer.offeredAt))
			}
			if first {
				receiver.firstCh <- err
				first = false
			}
		}
		producer := <-producerDone
		result.Offered, result.BufferDropped, result.PeakBuffered = producer.offered, producer.dropped, producer.peak
		result.ElapsedSeconds = time.Since(started).Seconds()
		if result.ElapsedSeconds > 0 {
			result.CommittedPerSec = float64(result.Committed) / result.ElapsedSeconds
		}
		slices.Sort(callLatencies)
		slices.Sort(offeredLatencies)
		if len(callLatencies) > 0 {
			result.P50CommitMS = pricingLatencyPercentile(callLatencies, 0.50)
			result.P95CommitMS = pricingLatencyPercentile(callLatencies, 0.95)
			result.P99CommitMS = pricingLatencyPercentile(callLatencies, 0.99)
			result.MaxCommitMS = callLatencies[len(callLatencies)-1].Seconds() * 1000
			result.P95OfferedToCommitMS = pricingLatencyPercentile(offeredLatencies, 0.95)
			result.MaxOfferedToCommitMS = offeredLatencies[len(offeredLatencies)-1].Seconds() * 1000
		}
		receiver.doneCh <- result
	}()
	return receiver
}

func pricingLatencyPercentile(sorted []time.Duration, fraction float64) float64 {
	index := int(float64(len(sorted)-1) * fraction)
	return sorted[index].Seconds() * 1000
}

func requireCompletePricingInbox(inbox pricingBenchInbox) error {
	if inbox.Errors != 0 || inbox.BufferDropped != 0 || inbox.Committed != inbox.Offered {
		return fmt.Errorf("offered=%d committed=%d write_errors=%d buffer_dropped=%d", inbox.Offered, inbox.Committed, inbox.Errors, inbox.BufferDropped)
	}
	return nil
}

func (r *pricingInboxReceiver) waitFirst(ctx context.Context) error {
	select {
	case err := <-r.firstCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *pricingInboxReceiver) stop() pricingBenchInbox {
	r.stopMu.Do(func() {
		close(r.stopCh)
		r.result = <-r.doneCh
	})
	return r.result
}

// verifyPricingBenchInbox 流式核对所有成功写入的原 ID、原文和 SHA256，不把千万行载入内存。
func verifyPricingBenchInbox(ctx context.Context, reader *gorm.DB, source string, want int64) error {
	sqlDB, err := reader.DB()
	if err != nil {
		return err
	}
	rows, err := sqlDB.QueryContext(ctx, "SELECT id, message_hash, raw_message FROM redis_usage_inboxes WHERE source = ? ORDER BY id", source)
	if err != nil {
		return err
	}
	defer rows.Close()
	var count, lastID int64
	for rows.Next() {
		var id int64
		var hash, raw string
		if err := rows.Scan(&id, &hash, &raw); err != nil {
			return err
		}
		sum := sha256.Sum256([]byte(raw))
		if id <= lastID || hash != hex.EncodeToString(sum[:]) {
			return fmt.Errorf("pricing benchmark inbox ID/hash mismatch at row %d", count+1)
		}
		count++
		lastID = id
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if count != want {
		return fmt.Errorf("pricing benchmark inbox rows=%d, committed=%d", count, want)
	}
	return nil
}

// verifyPricingUpgradeCounts 检查冷热与费用状态、水位；全量费用一致性由真实 M6 已完成。
func verifyPricingUpgradeCounts(ctx context.Context, reader *gorm.DB, spec datasetSpec, report *pricingBenchReport) error {
	for _, target := range []struct {
		table string
		want  int64
	}{{"usage_events", spec.Hot}, {"usage_events_archive", spec.Archive}} {
		var count, nullCost int64
		if err := reader.Table(target.table).Count(&count).Error; err != nil {
			return err
		}
		if count != target.want {
			return fmt.Errorf("post-upgrade %s rows=%d want=%d", target.table, count, target.want)
		}
		if err := reader.Table(target.table).Where("cost_usd IS NULL OR cost_available IS NULL").Count(&nullCost).Error; err != nil {
			return err
		}
		report.PostUpgradeCostNullRows += nullCost
	}
	if report.PostUpgradeCostNullRows != 0 {
		return fmt.Errorf("post-upgrade cost columns contain %d NULL rows", report.PostUpgradeCostNullRows)
	}
	if err := reader.WithContext(ctx).Table("usage_aggregation_checkpoints").Select("last_aggregated_usage_event_id").Where("name = 'overview'").Scan(&report.PostUpgradeOverviewCursor).Error; err != nil {
		return err
	}
	if report.PostUpgradeOverviewCursor != spec.Events {
		return fmt.Errorf("post-upgrade overview cursor=%d want=%d", report.PostUpgradeOverviewCursor, spec.Events)
	}
	facts, err := pricingBenchStoredFacts(ctx, reader)
	if err != nil {
		return err
	}
	if err := comparePricingBenchFacts(spec, facts); err != nil {
		return fmt.Errorf("post-upgrade facts: %w", err)
	}
	report.PostUpgradeTotalTokens = facts.TotalTokens
	report.PostUpgradeEventCostUSD = facts.EventCostUSD
	return nil
}

type pricingBenchFacts struct {
	TotalTokens, HourlyRequests, DailyRequests int64
	EventCostUSD, HourlyCostUSD, DailyCostUSD  float64
}

// pricingBenchStoredFacts 用数据库聚合核对全量Token、请求数和费用，不把原始事件载入内存。
func pricingBenchStoredFacts(ctx context.Context, reader *gorm.DB) (pricingBenchFacts, error) {
	var facts pricingBenchFacts
	sqlDB, err := reader.DB()
	if err != nil {
		return facts, err
	}
	for _, table := range []string{"usage_events", "usage_events_archive"} {
		var tokens int64
		var cost float64
		if err := sqlDB.QueryRowContext(ctx, "SELECT COALESCE(SUM(total_tokens),0), COALESCE(SUM(cost_usd),0) FROM "+table).Scan(&tokens, &cost); err != nil {
			return facts, err
		}
		facts.TotalTokens += tokens
		facts.EventCostUSD += cost
	}
	for _, target := range []struct {
		table    string
		requests *int64
		cost     *float64
	}{{"usage_overview_hourly_stats", &facts.HourlyRequests, &facts.HourlyCostUSD}, {"usage_overview_daily_stats", &facts.DailyRequests, &facts.DailyCostUSD}} {
		if err := sqlDB.QueryRowContext(ctx, "SELECT COALESCE(SUM(request_count),0), COALESCE(SUM(cost_usd),0) FROM "+target.table).Scan(target.requests, target.cost); err != nil {
			return facts, err
		}
	}
	return facts, nil
}

func comparePricingBenchFacts(spec datasetSpec, facts pricingBenchFacts) error {
	if facts.TotalTokens != spec.TotalTokens || facts.HourlyRequests != spec.HourlyRequests || facts.DailyRequests != spec.DailyRequests {
		return fmt.Errorf("total tokens/hourly requests/daily requests changed: got %d/%d/%d want %d/%d/%d",
			facts.TotalTokens, facts.HourlyRequests, facts.DailyRequests, spec.TotalTokens, spec.HourlyRequests, spec.DailyRequests)
	}
	for _, target := range []struct {
		name  string
		value float64
	}{{"hourly", facts.HourlyCostUSD}, {"daily", facts.DailyCostUSD}} {
		if math.Abs(target.value-facts.EventCostUSD) > math.Max(1e-6, math.Abs(facts.EventCostUSD)*1e-10) {
			return fmt.Errorf("%s total cost %g differs from event cost %g", target.name, target.value, facts.EventCostUSD)
		}
	}
	return nil
}

// runPricingRecalculationBenchmark 使用 App 同款 service 依赖和 Gin 路由；并发费用读取必须经过 CostReadGate。
func runPricingRecalculationBenchmark(ctx context.Context, writer, reader *gorm.DB, writerSQL *sql.DB, spec datasetSpec, options pricingBenchOptions, report *pricingBenchReport) error {
	snapshot, err := repository.LoadPricingSnapshot(ctx, writer)
	if err != nil {
		return err
	}
	catalog := pricing.NewCatalog(snapshot)
	recent, err := repository.NewUsageRecentEventCache(writer, repository.UsageRecentEventCacheOptions{Now: func() time.Time { return options.Anchor }})
	if err != nil {
		return err
	}
	defer recent.Close()
	quotas := quota.NewServiceWithRegistry(writer, quota.NewProviderRegistry(nil))
	defer quotas.StopRefreshTasks()
	aggregation := poller.NewUsageAggregationRunner(writer)
	syncer := service.NewSyncServiceWithOptions(writer, service.SyncServiceOptions{
		PricingCatalog: catalog, RecentUsageEvents: recent, UsageAggregationNotifier: aggregation,
		BaseURL: "https://synthetic.invalid", Now: func() time.Time { return options.Anchor },
	})
	readGate := service.NewCostReadGate()
	lifecycle, cancel := context.WithCancel(ctx)
	provider := service.NewPricingServiceWithRecalculation(writer, catalog, service.PricingRecalculationDependencies{
		LifecycleCtx: lifecycle, Sync: syncer, Aggregation: aggregation, CostReadGate: readGate,
		Recent: recent, Quota: quotas, Now: func() time.Time { return options.Anchor },
	})
	defer func() {
		cancel()
		provider.WaitPricingRecalculation()
	}()
	usage := service.NewUsageServiceWithRecentCache(writer, recent, catalog)
	router := keeperapi.NewRouter(webui.Static, nil, usage, provider, keeperapi.AuthConfig{}, nil, "", keeperapi.OptionalProviders{CostReadGate: readGate})
	startDay := options.Anchor.Add(-120 * 24 * time.Hour).Format("2006-01-02")
	endDay := options.Anchor.Format("2006-01-02")
	querySuffix := "?range=custom&unit=day&start=" + startDay + "&end=" + endDay
	overviewURL := "/api/v1/usage/overview" + querySuffix
	analysisURL := "/api/v1/usage/analysis" + querySuffix
	report.Queries.OverviewURL = overviewURL
	report.Queries.AnalysisURL = analysisURL
	report.Queries.BeforePhase = "post_upgrade_before_recalculation"
	report.Queries.AfterPhase = "post_recalculation_and_inbox_catchup"
	fmt.Println("[pricingbench] normal 120-day Overview and Analysis queries")
	if report.Queries.BeforeOverviewMS, err = pricingBenchAPIQuery(router, overviewURL, spec.Events); err != nil {
		return fmt.Errorf("before-recalculation overview query: %w", err)
	}
	if report.Queries.BeforeAnalysisMS, err = pricingBenchAPIQuery(router, analysisURL, spec.Events); err != nil {
		return fmt.Errorf("before-recalculation analysis query: %w", err)
	}
	firstConfig, ok := snapshot.PricingModelConfig("bench-model-001")
	if !ok {
		return fmt.Errorf("benchmark price for bench-model-001 is missing")
	}
	firstConfig.BasePrices.Input += 0.1
	saved, err := provider.SavePricingModel(ctx, firstConfig)
	if err != nil {
		return fmt.Errorf("save changed benchmark model price: %w", err)
	}
	startAt := options.Anchor.Add(-30 * 24 * time.Hour)
	var expectedTarget int64
	if err := reader.WithContext(ctx).Table("usage_events").Where("timestamp >= ? AND timestamp < ?", timeutil.FormatStorageTime(startAt), timeutil.FormatStorageTime(options.Anchor)).Count(&expectedTarget).Error; err != nil {
		return fmt.Errorf("count actual recalculation target: %w", err)
	}
	if expectedTarget <= 0 {
		return fmt.Errorf("pricing benchmark has no 30-day target events")
	}
	report.Recalculation.StartAt, report.Recalculation.EndAt = startAt, options.Anchor
	report.Recalculation.UpdatedModel = firstConfig.Model
	report.Recalculation.ConfigRevision = saved.ConfigRevision
	fmt.Println("[pricingbench] 30-day recalculation with concurrent durable inbox and gated statistics queries")
	stopRecalcTrace, err := startSQLiteWriterTrace(ctx, writer)
	if err != nil {
		return fmt.Errorf("start recalculation writer trace: %w", err)
	}
	recalcTraceActive := true
	defer func() {
		if recalcTraceActive {
			_, _ = stopRecalcTrace(context.Background())
		}
	}()
	monitor := startPricingResourceMonitor(spec.Path, writerSQL, reader, time.Now(), map[string]float64{})
	receiver := startPricingInboxReceiver(ctx, writer, options.Anchor, options.ReceiveInterval, false)
	if err := receiver.waitFirst(ctx); err != nil {
		report.RecalculationInbox = receiver.stop()
		report.RecalculationResources = monitor.stop()
		return err
	}
	recalcStarted := time.Now()
	started, err := provider.StartPricingRecalculation(ctx, servicedto.StartRecalculationRequest{StartAt: startAt, ConfigRevision: saved.ConfigRevision})
	if err != nil {
		report.RecalculationInbox = receiver.stop()
		report.RecalculationResources = monitor.stop()
		return fmt.Errorf("start real pricing recalculation: %w", err)
	}
	if !started.Started {
		report.RecalculationInbox = receiver.stop()
		report.RecalculationResources = monitor.stop()
		return fmt.Errorf("real pricing recalculation did not start")
	}
	stopQueries, queryResults := pricingBenchConcurrentQueries(router, overviewURL)
	provider.WaitPricingRecalculation()
	report.Recalculation.WallSeconds = time.Since(recalcStarted).Seconds()
	stopQueries()
	counts := <-queryResults
	report.Queries.DuringOK, report.Queries.DuringCostsBusy, report.Queries.DuringErrors = counts.DuringOK, counts.DuringCostsBusy, counts.DuringErrors
	if report.Queries.DuringErrors != 0 {
		report.RecalculationInbox = receiver.stop()
		report.RecalculationResources = monitor.stop()
		return fmt.Errorf("concurrent pricing queries returned %d unexpected responses", report.Queries.DuringErrors)
	}
	report.RecalculationInbox = receiver.stop()
	report.RecalculationResources = monitor.stop()
	if err := requireCompletePricingInbox(report.RecalculationInbox); err != nil {
		return fmt.Errorf("synthetic arrivals during recalculation: %w", err)
	}
	report.RecalculationTrace, err = stopRecalcTrace(ctx)
	recalcTraceActive = false
	if err != nil {
		return fmt.Errorf("stop recalculation writer trace: %w", err)
	}
	current, err := provider.CurrentPricingRecalculation(ctx)
	if err != nil {
		return fmt.Errorf("load completed pricing task: %w", err)
	}
	if current == nil {
		return fmt.Errorf("completed pricing task is missing")
	}
	report.Recalculation.Status = string(current.Status)
	report.Recalculation.ProcessedRows = current.ProcessedCount
	if current.TotalCount != nil {
		report.Recalculation.TargetRows = *current.TotalCount
	}
	if report.Recalculation.WallSeconds > 0 {
		report.Recalculation.RowsPerSecond = float64(current.ProcessedCount) / report.Recalculation.WallSeconds
	}
	if string(current.Status) != "completed" || current.ProcessedCount != expectedTarget || current.TotalCount == nil || *current.TotalCount != expectedTarget {
		return fmt.Errorf("pricing recalculation target/process mismatch: status=%s target=%d reported=%d processed=%d", current.Status, expectedTarget, report.Recalculation.TargetRows, current.ProcessedCount)
	}
	facts, err := pricingBenchStoredFacts(ctx, reader)
	if err != nil {
		return err
	}
	if err := comparePricingBenchFacts(spec, facts); err != nil {
		return fmt.Errorf("post-recalculation facts: %w", err)
	}
	report.PostRecalcTotalTokens = facts.TotalTokens
	report.PostRecalcEventCostUSD = facts.EventCostUSD
	if err := verifyPricingBenchInbox(ctx, reader, "pricingbench:usage", report.MigrationInbox.Committed+report.RecalculationInbox.Committed); err != nil {
		return fmt.Errorf("verify durable inbox after recalculation: %w", err)
	}
	report.RecalculationInbox.VerifiedRows = report.RecalculationInbox.Committed
	fmt.Println("[pricingbench] process retained inbox once, then catch up Overview")
	drainStarted := time.Now()
	for {
		batch, err := syncer.ProcessRedisUsageInbox(ctx)
		if err != nil {
			return fmt.Errorf("process pricing benchmark inbox: %w", err)
		}
		if batch == nil || batch.Empty || batch.ProcessedRows == 0 {
			break
		}
		report.Catchup.ProcessedEvents += int64(batch.InsertedEvents)
	}
	if err := repository.AggregateUsageOverviewStats(ctx, writer, options.Anchor); err != nil {
		return fmt.Errorf("catch up benchmark Overview: %w", err)
	}
	report.Catchup.WallSeconds = time.Since(drainStarted).Seconds()
	if report.Catchup.WallSeconds > 0 {
		report.Catchup.EventsPerSecond = float64(report.Catchup.ProcessedEvents) / report.Catchup.WallSeconds
	}
	if report.Catchup.ProcessedEvents != report.MigrationInbox.Committed+report.RecalculationInbox.Committed {
		return fmt.Errorf("inbox catchup events=%d committed messages=%d", report.Catchup.ProcessedEvents, report.MigrationInbox.Committed+report.RecalculationInbox.Committed)
	}
	queryExpected := spec.Events + report.Catchup.ProcessedEvents
	if report.Queries.AfterOverviewMS, err = pricingBenchAPIQuery(router, overviewURL, queryExpected); err != nil {
		return fmt.Errorf("after-recalculation overview query: %w", err)
	}
	if report.Queries.AfterAnalysisMS, err = pricingBenchAPIQuery(router, analysisURL, queryExpected); err != nil {
		return fmt.Errorf("after-recalculation analysis query: %w", err)
	}
	fmt.Printf("[pricingbench] completed; recalculated=%d caught_up=%d busy_reads=%d\n", report.Recalculation.ProcessedRows, report.Catchup.ProcessedEvents, report.Queries.DuringCostsBusy)
	return nil
}

// pricingBenchAPIQuery 核对120天响应内的请求总数，避免只把HTTP 200当作统计正确。
func pricingBenchAPIQuery(router http.Handler, target string, expected int64) (float64, error) {
	request := httptest.NewRequest(http.MethodGet, target, nil)
	response := httptest.NewRecorder()
	started := time.Now()
	router.ServeHTTP(response, request)
	elapsed := time.Since(started).Seconds() * 1000
	if response.Code != http.StatusOK {
		return elapsed, fmt.Errorf("GET %s returned HTTP %d", strings.Split(target, "?")[0], response.Code)
	}
	var count int64
	if strings.Contains(target, "/usage/analysis") {
		var body struct {
			TokenUsage []struct {
				Requests int64 `json:"requests"`
			} `json:"token_usage"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			return elapsed, err
		}
		for _, bucket := range body.TokenUsage {
			count += bucket.Requests
		}
	} else {
		var body struct {
			Usage struct {
				TotalRequests int64 `json:"total_requests"`
			} `json:"usage"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			return elapsed, err
		}
		count = body.Usage.TotalRequests
	}
	if count != expected {
		return elapsed, fmt.Errorf("GET %s requests=%d want=%d", strings.Split(target, "?")[0], count, expected)
	}
	return elapsed, nil
}

// pricingBenchConcurrentQueries 通过真实路由计数成功与成本门禁拒绝，不绕过 gate 读取混合费用。
func pricingBenchConcurrentQueries(router http.Handler, target string) (func(), <-chan pricingBenchQueries) {
	stopCh := make(chan struct{})
	results := make(chan pricingBenchQueries, 1)
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		var counts pricingBenchQueries
		for {
			select {
			case <-stopCh:
				results <- counts
				return
			case <-ticker.C:
				request := httptest.NewRequest(http.MethodGet, target, nil)
				response := httptest.NewRecorder()
				router.ServeHTTP(response, request)
				switch response.Code {
				case http.StatusOK:
					counts.DuringOK++
				case http.StatusServiceUnavailable:
					if strings.Contains(response.Body.String(), `"costs_busy"`) {
						counts.DuringCostsBusy++
					} else {
						counts.DuringErrors++
					}
				default:
					counts.DuringErrors++
				}
			}
		}
	}()
	return func() { close(stopCh) }, results
}
