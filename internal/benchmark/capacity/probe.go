package capacity

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cpa-usage-keeper/internal/repository"
	_ "github.com/mattn/go-sqlite3"
)

type UsagePayloadMetadata struct {
	APIKeyIndex   int    `json:"api_key_index"`
	ModelIndex    int    `json:"model_index"`
	IdentityIndex int    `json:"identity_index"`
	APIGroupKey   string `json:"api_group_key"`
	Model         string `json:"model"`
	AuthIndex     string `json:"auth_index"`
}

type usagePayload struct {
	Timestamp string             `json:"timestamp"`
	LatencyMS int64              `json:"latency_ms"`
	TTFTMS    int64              `json:"ttft_ms"`
	Source    string             `json:"source"`
	AuthIndex string             `json:"auth_index"`
	Tokens    usagePayloadTokens `json:"tokens"`
	Provider  string             `json:"provider"`
	Model     string             `json:"model"`
	AuthType  string             `json:"auth_type"`
	APIKey    string             `json:"api_key"`
	RequestID string             `json:"request_id"`
	Endpoint  string             `json:"endpoint"`
}

type usagePayloadTokens struct {
	InputTokens         int64 `json:"input_tokens"`
	OutputTokens        int64 `json:"output_tokens"`
	ReasoningTokens     int64 `json:"reasoning_tokens"`
	CachedTokens        int64 `json:"cached_tokens"`
	CacheReadTokens     int64 `json:"cache_read_tokens"`
	CacheCreationTokens int64 `json:"cache_creation_tokens"`
	TotalTokens         int64 `json:"total_tokens"`
}

type LatencySummary struct {
	P50MS float64 `json:"p50_ms"`
	P95MS float64 `json:"p95_ms"`
	P99MS float64 `json:"p99_ms"`
	MaxMS float64 `json:"max_ms"`
}

type DashboardLatencySample struct {
	Path     string
	Duration time.Duration
}

const heavyDashboardLatencyPath = "/api/v1/usage/analysis/latency?range=30d"

var dashboardReplayPaths = []string{
	"/api/v1/usage/overview?range=30d",
	"/api/v1/usage/overview/realtime?range=1h",
	"/api/v1/usage/activity?range=30d",
	"/api/v1/usage/analysis?range=30d",
	"/api/v1/usage/analysis/latency?range=30d",
	"/api/v1/usage/events?range=30d&page=1&page_size=50",
}

type ProbeOptions struct {
	RedisAddress      string
	RedisPassword     string
	RedisChannel      string
	ApplicationURL    string
	DatabasePath      string
	RatePerSecond     int
	Duration          time.Duration
	DrainTimeout      time.Duration
	HTTPRatePerSecond int
	Cardinality       Cardinality
	APIKeyProfiles    []APIKeyProfile
	Seed              uint64
	Thresholds        ProbeThresholds
}

type ProbeReport struct {
	StartedAt            time.Time                 `json:"started_at"`
	FinishedAt           time.Time                 `json:"finished_at"`
	RatePerSecond        int                       `json:"rate_per_second"`
	Metrics              ProbeMetrics              `json:"metrics"`
	Evaluation           ProbeEvaluation           `json:"evaluation"`
	Latency              LatencySummary            `json:"latency"`
	CoreLatency          LatencySummary            `json:"core_latency"`
	LatencyByPath        map[string]LatencySummary `json:"latency_by_path"`
	DrainSeconds         float64                   `json:"drain_seconds"`
	FinalBacklog         int64                     `json:"final_backlog"`
	FinalCheckpointLag   int64                     `json:"final_checkpoint_lag"`
	FinalIdentityPending int64                     `json:"final_identity_pending"`
	PublishedByTier      map[string]int64          `json:"published_by_tier"`
	PublishError         string                    `json:"publish_error,omitempty"`
}

type databaseProbeState struct {
	Backlog         int64
	MaxEventID      int64
	CheckpointMin   int64
	IdentityPending int64
}

func BuildUsagePayload(sequence int64, timestamp time.Time, cardinality Cardinality, apiProfiles []APIKeyProfile, seed uint64) ([]byte, UsagePayloadMetadata, error) {
	if sequence <= 0 {
		return nil, UsagePayloadMetadata{}, fmt.Errorf("usage payload sequence must be positive")
	}
	if err := validateCardinality("probe", cardinality); err != nil {
		return nil, UsagePayloadMetadata{}, err
	}
	if len(apiProfiles) != cardinality.APIKeys {
		return nil, UsagePayloadMetadata{}, fmt.Errorf("API key profile count=%d, want %d", len(apiProfiles), cardinality.APIKeys)
	}
	traffic, err := newAPITrafficTopology(apiProfiles)
	if err != nil {
		return nil, UsagePayloadMetadata{}, err
	}
	return buildUsagePayload(sequence, timestamp, cardinality, traffic, seed)
}

func buildUsagePayload(sequence int64, timestamp time.Time, cardinality Cardinality, traffic apiTrafficTopology, seed uint64) ([]byte, UsagePayloadMetadata, error) {
	random := splitMix64{state: seed ^ uint64(sequence)*0x9e3779b97f4a7c15}
	apiIndex := traffic.choose(timestamp, &random)
	identityIndex := chooseCorrelatedIdentity(apiIndex, cardinality, timestamp, &random)
	modelIndex := chooseCorrelatedModel(identityIndex, cardinality, &random)
	authType := "oauth"
	provider := "codex"
	if (identityIndex+1)%4 == 0 {
		authType = "apikey"
		provider = "openai"
	}
	input := int64(100 + random.next()%3000)
	output := int64(20 + random.next()%900)
	reasoning := int64(random.next() % uint64(output+1))
	cacheRead := int64(random.next() % uint64(max(input/3, 1)))
	cacheCreation := int64(random.next() % uint64(max(input/8, 1)))
	total := input + output
	metadata := UsagePayloadMetadata{
		APIKeyIndex: apiIndex + 1, ModelIndex: modelIndex + 1, IdentityIndex: identityIndex + 1,
		APIGroupKey: strconv.Itoa(apiIndex + 1), Model: fmt.Sprintf("bench-model-%03d", modelIndex+1),
		AuthIndex: fmt.Sprintf("bench-auth-%04d", identityIndex+1),
	}
	payload := usagePayload{
		Timestamp: timestamp.Format(time.RFC3339Nano), LatencyMS: int64(300 + random.next()%30_000),
		TTFTMS: int64(50 + random.next()%5_000), Source: provider, AuthIndex: metadata.AuthIndex,
		Tokens: usagePayloadTokens{
			InputTokens: input, OutputTokens: output, ReasoningTokens: reasoning,
			CachedTokens: cacheRead, CacheReadTokens: cacheRead,
			CacheCreationTokens: cacheCreation, TotalTokens: total,
		},
		Provider: provider, Model: metadata.Model, AuthType: authType, APIKey: metadata.APIGroupKey,
		RequestID: fmt.Sprintf("bench-live-%012d", sequence), Endpoint: []string{"responses", "chat/completions", "messages"}[modelIndex%3],
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, UsagePayloadMetadata{}, fmt.Errorf("encode usage payload: %w", err)
	}
	return data, metadata, nil
}

func LatencyPercentiles(values []time.Duration) LatencySummary {
	if len(values) == 0 {
		return LatencySummary{}
	}
	ordered := append([]time.Duration(nil), values...)
	sort.Slice(ordered, func(left, right int) bool { return ordered[left] < ordered[right] })
	nearest := func(percentile float64) time.Duration {
		index := int(mathCeil(percentile*float64(len(ordered)))) - 1
		if index < 0 {
			index = 0
		}
		if index >= len(ordered) {
			index = len(ordered) - 1
		}
		return ordered[index]
	}
	toMS := func(value time.Duration) float64 { return float64(value) / float64(time.Millisecond) }
	return LatencySummary{P50MS: toMS(nearest(0.50)), P95MS: toMS(nearest(0.95)), P99MS: toMS(nearest(0.99)), MaxMS: toMS(ordered[len(ordered)-1])}
}

func mathCeil(value float64) float64 {
	integer := int64(value)
	if float64(integer) == value {
		return float64(integer)
	}
	return float64(integer + 1)
}

func RunProbe(ctx context.Context, options ProbeOptions) (ProbeReport, error) {
	if options.RatePerSecond <= 0 || options.Duration <= 0 || strings.TrimSpace(options.DatabasePath) == "" {
		return ProbeReport{}, fmt.Errorf("probe rate, duration, and database are required")
	}
	if err := validateCardinality("probe", options.Cardinality); err != nil {
		return ProbeReport{}, err
	}
	if len(options.APIKeyProfiles) != options.Cardinality.APIKeys {
		return ProbeReport{}, fmt.Errorf("API key profile count=%d, want %d", len(options.APIKeyProfiles), options.Cardinality.APIKeys)
	}
	if options.RedisChannel == "" {
		options.RedisChannel = "usage"
	}
	if options.DrainTimeout <= 0 {
		options.DrainTimeout = 15 * time.Second
	}
	database, err := openProbeDatabase(options.DatabasePath)
	if err != nil {
		return ProbeReport{}, err
	}
	defer database.Close()
	startState, err := loadDatabaseProbeState(ctx, database, -1)
	if err != nil {
		return ProbeReport{}, err
	}
	publisher, err := newRedisPublisher(ctx, options.RedisAddress, options.RedisPassword)
	if err != nil {
		return ProbeReport{}, err
	}
	defer publisher.Close()
	traffic, err := newAPITrafficTopology(options.APIKeyProfiles)
	if err != nil {
		return ProbeReport{}, err
	}

	report := ProbeReport{StartedAt: time.Now(), RatePerSecond: options.RatePerSecond, PublishedByTier: map[string]int64{}}
	probeContext, cancel := context.WithTimeout(ctx, options.Duration)
	defer cancel()
	var httpErrors atomic.Int64
	latencyDone := make(chan []DashboardLatencySample, 1)
	go func() {
		latencyDone <- runHTTPReplay(probeContext, options.ApplicationURL, options.HTTPRatePerSecond, &httpErrors)
	}()

	sequence := int64(0)
	start := time.Now()
	targetTotal := int64(options.Duration.Seconds() * float64(options.RatePerSecond))
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	publishErrors := int64(0)
	for {
		select {
		case <-probeContext.Done():
			goto finishedPublishing
		case now := <-ticker.C:
			// 第一个事件在窗口起点发送，最后一个目标事件会在 duration 结束前一个频率间隔进入队列。
			target := min(1+int64(now.Sub(start).Seconds()*float64(options.RatePerSecond)), targetTotal)
			if target <= sequence {
				continue
			}
			batchSize := min(target-sequence, int64(1000))
			payloads := make([][]byte, 0, batchSize)
			metadata := make([]UsagePayloadMetadata, 0, batchSize)
			for index := int64(0); index < batchSize; index++ {
				payload, item, err := buildUsagePayload(sequence+index+1, now, options.Cardinality, traffic, options.Seed)
				if err != nil {
					return ProbeReport{}, err
				}
				payloads = append(payloads, payload)
				metadata = append(metadata, item)
			}
			published, publishErr := publisher.PublishBatch(probeContext, options.RedisChannel, payloads)
			for index := 0; index < published; index++ {
				report.PublishedByTier[options.APIKeyProfiles[metadata[index].APIKeyIndex-1].Tier]++
			}
			sequence += int64(published)
			if publishErr != nil {
				// Redis 写超时属于该 offered rate 的有效容量失败；保留已发布数量并进入 drain，不能把整个 cell 误判为基础设施失败。
				publishErrors++
				report.PublishError = publishErr.Error()
				cancel()
				goto finishedPublishing
			}
		}
	}

finishedPublishing:
	latencySamples := <-latencyDone
	immediateState, err := loadDatabaseProbeState(ctx, database, startState.MaxEventID)
	if err != nil {
		return ProbeReport{}, err
	}
	drainStart := time.Now()
	finalState := immediateState
	for time.Since(drainStart) < options.DrainTimeout {
		if finalState.Backlog == 0 && finalState.CheckpointMin >= finalState.MaxEventID && finalState.IdentityPending == 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
		finalState, err = loadDatabaseProbeState(ctx, database, startState.MaxEventID)
		if err != nil {
			return ProbeReport{}, err
		}
	}
	report.FinishedAt = time.Now()
	report.DrainSeconds = time.Since(drainStart).Seconds()
	report.FinalBacklog = finalState.Backlog
	report.FinalCheckpointLag = max(finalState.MaxEventID-finalState.CheckpointMin, 0)
	report.FinalIdentityPending = finalState.IdentityPending
	durableEvents, err := countNewDurableEvents(ctx, database, startState.MaxEventID)
	if err != nil {
		return ProbeReport{}, err
	}
	expectedEvents := targetTotal
	report.Metrics = ProbeMetrics{
		OfferedEvents: expectedEvents, PublishedEvents: sequence, DurableEvents: durableEvents,
		BacklogStart: startState.Backlog, BacklogEnd: finalState.Backlog,
		Errors: publishErrors + httpErrors.Load(), HTTPRequests: int64(len(latencySamples)),
		CheckpointLag: report.FinalCheckpointLag, IdentityPending: report.FinalIdentityPending,
	}
	report.Latency, report.CoreLatency, report.LatencyByPath = SummarizeDashboardLatencies(latencySamples)
	report.Metrics.HTTPP95MS = report.CoreLatency.P95MS
	report.Metrics.HTTPP99MS = report.Latency.P99MS
	report.Evaluation = EvaluateProbe(report.Metrics, options.Thresholds)
	return report, nil
}

func openProbeDatabase(path string) (*sql.DB, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve probe database path: %w", err)
	}
	dsn := repository.BuildSQLiteFileURI(absolute) + "?mode=ro&_query_only=on&_busy_timeout=5000"
	database, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open probe database: %w", err)
	}
	if err := database.Ping(); err != nil {
		database.Close()
		return nil, fmt.Errorf("ping probe database: %w", err)
	}
	return database, nil
}

func loadDatabaseProbeState(ctx context.Context, database *sql.DB, identityFloor int64) (databaseProbeState, error) {
	state := databaseProbeState{}
	if err := database.QueryRowContext(ctx, "SELECT COALESCE(MAX(id), 0) FROM usage_events").Scan(&state.MaxEventID); err != nil {
		return state, fmt.Errorf("load probe event state: %w", err)
	}
	if identityFloor < 0 {
		identityFloor = state.MaxEventID
	}
	if err := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM redis_usage_inboxes WHERE status IN ('pending', 'process_failed')").Scan(&state.Backlog); err != nil {
		return state, fmt.Errorf("load probe inbox state: %w", err)
	}
	if err := database.QueryRowContext(ctx, "SELECT COALESCE(MIN(last_aggregated_usage_event_id), 0) FROM usage_aggregation_checkpoints").Scan(&state.CheckpointMin); err != nil {
		return state, fmt.Errorf("load probe checkpoint state: %w", err)
	}
	if err := database.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1
			FROM usage_events AS event
			JOIN usage_identities AS identity ON identity.identity = event.auth_index
			WHERE event.id > ?
			  AND event.id > identity.last_aggregated_usage_event_id
			LIMIT 1
		)`, identityFloor).Scan(&state.IdentityPending); err != nil {
		return state, fmt.Errorf("load probe identity state: %w", err)
	}
	return state, nil
}

func countNewDurableEvents(ctx context.Context, database *sql.DB, afterID int64) (int64, error) {
	var count int64
	if err := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM usage_events WHERE id > ?", afterID).Scan(&count); err != nil {
		return 0, fmt.Errorf("count durable probe events: %w", err)
	}
	return count, nil
}

func runHTTPReplay(ctx context.Context, baseURL string, rate int, errors *atomic.Int64) []DashboardLatencySample {
	if strings.TrimSpace(baseURL) == "" || rate <= 0 {
		return nil
	}
	client := &http.Client{Timeout: 10 * time.Second}
	latencies := make(chan DashboardLatencySample, rate*2+16)
	var wait sync.WaitGroup
	go func() {
		defer close(latencies)
		interval := time.Second / time.Duration(rate)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		semaphore := make(chan struct{}, 16)
		sequence := 0
		for {
			select {
			case <-ctx.Done():
				wait.Wait()
				return
			case <-ticker.C:
				path := dashboardReplayPaths[sequence%len(dashboardReplayPaths)]
				sequence++
				semaphore <- struct{}{}
				wait.Add(1)
				go func() {
					defer wait.Done()
					defer func() { <-semaphore }()
					started := time.Now()
					request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+path, nil)
					if err != nil {
						errors.Add(1)
						return
					}
					response, err := client.Do(request)
					if err != nil {
						if ctx.Err() == nil {
							errors.Add(1)
						}
						return
					}
					_, _ = io.Copy(io.Discard, response.Body)
					_ = response.Body.Close()
					if response.StatusCode < 200 || response.StatusCode >= 300 {
						errors.Add(1)
						return
					}
					latencies <- DashboardLatencySample{Path: path, Duration: time.Since(started)}
				}()
			}
		}
	}()
	var values []DashboardLatencySample
	for value := range latencies {
		values = append(values, value)
	}
	return values
}

func SummarizeDashboardLatencies(samples []DashboardLatencySample) (LatencySummary, LatencySummary, map[string]LatencySummary) {
	overallValues := make([]time.Duration, 0, len(samples))
	coreValues := make([]time.Duration, 0, len(samples))
	grouped := make(map[string][]time.Duration)
	for _, sample := range samples {
		overallValues = append(overallValues, sample.Duration)
		grouped[sample.Path] = append(grouped[sample.Path], sample.Duration)
		if sample.Path != heavyDashboardLatencyPath {
			coreValues = append(coreValues, sample.Duration)
		}
	}
	byPath := make(map[string]LatencySummary, len(grouped))
	for path, values := range grouped {
		byPath[path] = LatencyPercentiles(values)
	}
	return LatencyPercentiles(overallValues), LatencyPercentiles(coreValues), byPath
}

func WarmDashboard(ctx context.Context, baseURL string) (time.Duration, error) {
	started := time.Now()
	client := &http.Client{Timeout: 30 * time.Second}
	for _, path := range dashboardReplayPaths {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+path, nil)
		if err != nil {
			return time.Since(started), err
		}
		response, err := client.Do(request)
		if err != nil {
			return time.Since(started), fmt.Errorf("warm dashboard %s: %w", path, err)
		}
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return time.Since(started), fmt.Errorf("warm dashboard %s returned %s", path, response.Status)
		}
	}
	return time.Since(started), nil
}

type redisPublisher struct {
	connection net.Conn
	reader     *bufio.Reader
	writer     *bufio.Writer
}

func newRedisPublisher(ctx context.Context, address, password string) (*redisPublisher, error) {
	dialer := net.Dialer{Timeout: 5 * time.Second}
	connection, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("connect Redis publisher: %w", err)
	}
	publisher := &redisPublisher{connection: connection, reader: bufio.NewReader(connection), writer: bufio.NewWriter(connection)}
	if password != "" {
		if err := publisher.writeCommand("AUTH", password); err != nil {
			publisher.Close()
			return nil, err
		}
		if err := publisher.writer.Flush(); err != nil {
			publisher.Close()
			return nil, err
		}
		if err := publisher.readReply(); err != nil {
			publisher.Close()
			return nil, fmt.Errorf("authenticate Redis publisher: %w", err)
		}
	}
	return publisher, nil
}

func (publisher *redisPublisher) PublishBatch(_ context.Context, channel string, payloads [][]byte) (int, error) {
	// 单批 Redis I/O 使用独立上限，避免 30 秒 probe 结束前最后一个 tick 继承即将过期的整体 deadline。
	if err := publisher.connection.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return 0, err
	}
	for _, payload := range payloads {
		if err := publisher.writeCommand("PUBLISH", channel, string(payload)); err != nil {
			return 0, err
		}
	}
	if err := publisher.writer.Flush(); err != nil {
		return 0, err
	}
	for index := range payloads {
		if err := publisher.readReply(); err != nil {
			return index, err
		}
	}
	return len(payloads), nil
}

func (publisher *redisPublisher) writeCommand(parts ...string) error {
	if _, err := fmt.Fprintf(publisher.writer, "*%d\r\n", len(parts)); err != nil {
		return err
	}
	for _, part := range parts {
		if _, err := fmt.Fprintf(publisher.writer, "$%d\r\n%s\r\n", len(part), part); err != nil {
			return err
		}
	}
	return nil
}

func (publisher *redisPublisher) readReply() error {
	prefix, err := publisher.reader.ReadByte()
	if err != nil {
		return err
	}
	line, err := publisher.reader.ReadString('\n')
	if err != nil {
		return err
	}
	line = strings.TrimSpace(line)
	if prefix == '-' {
		return fmt.Errorf("Redis error: %s", line)
	}
	if prefix != '+' && prefix != ':' {
		return fmt.Errorf("unexpected Redis response prefix %q", prefix)
	}
	return nil
}

func (publisher *redisPublisher) Close() error {
	if publisher == nil || publisher.connection == nil {
		return nil
	}
	return publisher.connection.Close()
}

func marshalProbeReport(report ProbeReport) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
