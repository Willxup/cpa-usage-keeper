package capacity

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"cpa-usage-keeper/internal/activity"
	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/latency"
	"cpa-usage-keeper/internal/overview"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/gorm"
)

const usageEventInsertColumns = entities.UsageEventStorageColumns

const DatasetGeneratorVersion = "production-v6-token-canonical"

type GenerateOptions struct {
	Path            string
	HotEvents       int64
	ArchiveEvents   int64
	HotDays         int
	ArchiveDays     int
	FailureRate     float64
	Seed            uint64
	Now             time.Time
	Cardinality     Cardinality
	TrafficTiers    []TrafficTier
	InsertBatchSize int
	AggregatePage   int
	Vacuum          bool
}

type DatasetResult struct {
	Path                    string    `json:"-"`
	GeneratorVersion        string    `json:"generator_version,omitempty"`
	Seed                    uint64    `json:"seed,omitempty"`
	BenchmarkNow            time.Time `json:"benchmark_now,omitempty"`
	HotEvents               int64     `json:"hot_events"`
	ArchiveEvents           int64     `json:"archive_events"`
	TotalEvents             int64     `json:"total_events"`
	FailedEvents            int64     `json:"failed_events"`
	Identities              int64     `json:"identities"`
	Models                  int64     `json:"models"`
	APIKeys                 int64     `json:"api_keys"`
	UsedIdentities          int64     `json:"used_identities"`
	UsedModels              int64     `json:"used_models"`
	UsedAPIKeys             int64     `json:"used_api_keys"`
	OrphanIdentities        int64     `json:"orphan_identities"`
	OrphanModels            int64     `json:"orphan_models"`
	OrphanAPIKeys           int64     `json:"orphan_api_keys"`
	DuplicateEventKeys      int64     `json:"duplicate_event_keys"`
	TokenSemanticViolations int64     `json:"token_semantic_violations"`
	OverviewHourlyRequests  int64     `json:"overview_hourly_requests"`
	OverviewDailyRequests   int64     `json:"overview_daily_requests"`
	IdentityRequests        int64     `json:"identity_requests"`
	OverviewHourlyRows      int64     `json:"overview_hourly_rows"`
	OverviewDailyRows       int64     `json:"overview_daily_rows"`
	ActivityRows            int64     `json:"activity_rows"`
	LatencyRows             int64     `json:"latency_rows"`
	InputTokens             int64     `json:"input_tokens"`
	OutputTokens            int64     `json:"output_tokens"`
	TotalLatencyMS          int64     `json:"total_latency_ms"`
	CheckpointMin           int64     `json:"checkpoint_min"`
	CheckpointMax           int64     `json:"checkpoint_max"`
	QuickCheck              string    `json:"quick_check"`
	DatabaseBytes           int64     `json:"database_bytes"`
	SemanticFingerprint     string    `json:"semantic_fingerprint"`
}

type storedIndex struct {
	Name string
	SQL  string
}

type generatedEvent struct {
	ID                  int64
	EventKey            string
	APIGroupKey         string
	Provider            string
	Endpoint            string
	AuthType            string
	RequestID           string
	Model               string
	ModelAlias          string
	ReasoningEffort     string
	ServiceTier         string
	ResponseServiceTier string
	ExecutorType        string
	Timestamp           time.Time
	Source              string
	AuthIndex           string
	Failed              bool
	LatencyMS           int64
	TTFTMS              int64
	InputTokens         int64
	OutputTokens        int64
	ReasoningTokens     int64
	CachedTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	TotalTokens         int64
}

type identityProfile struct {
	Identity string
	AuthType entities.UsageIdentityAuthType
}

type weightedSelector struct {
	cumulative []float64
	total      float64
}

type timeBucket struct {
	start time.Time
	count int64
}

type eventTimeline struct {
	buckets     []timeBucket
	bucketIndex int
	inside      int64
}

func GenerateDataset(ctx context.Context, options GenerateOptions) (DatasetResult, error) {
	if err := validateGenerateOptions(options); err != nil {
		return DatasetResult{}, err
	}
	if _, err := os.Stat(options.Path); err == nil {
		return DatasetResult{}, fmt.Errorf("benchmark dataset already exists: %s", filepath.Clean(options.Path))
	} else if !os.IsNotExist(err) {
		return DatasetResult{}, fmt.Errorf("inspect benchmark dataset path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(options.Path), 0o755); err != nil {
		return DatasetResult{}, fmt.Errorf("create benchmark dataset directory: %w", err)
	}

	db, err := repository.OpenDatabase(config.Config{SQLitePath: options.Path})
	if err != nil {
		return DatasetResult{}, fmt.Errorf("open benchmark dataset: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return DatasetResult{}, fmt.Errorf("resolve benchmark database: %w", err)
	}
	defer sqlDB.Close()

	// 数据准备可重建且不计入容量，使用 NORMAL 和较大页缓存减少 10M 行合成时间。
	if err := db.Exec("PRAGMA synchronous=NORMAL").Error; err != nil {
		return DatasetResult{}, fmt.Errorf("configure benchmark synchronous mode: %w", err)
	}
	if err := db.Exec("PRAGMA temp_store=MEMORY").Error; err != nil {
		return DatasetResult{}, fmt.Errorf("configure benchmark temp store: %w", err)
	}
	if err := db.Exec("PRAGMA cache_size=-524288").Error; err != nil {
		return DatasetResult{}, fmt.Errorf("configure benchmark page cache: %w", err)
	}

	indexes, err := dropSecondaryIndexes(ctx, sqlDB, "usage_events")
	if err != nil {
		return DatasetResult{}, err
	}
	identities, err := seedDatasetMetadata(db, options)
	if err != nil {
		return DatasetResult{}, err
	}
	if err := insertGeneratedEvents(ctx, sqlDB, options, identities); err != nil {
		return DatasetResult{}, err
	}
	if err := restoreIndexes(ctx, sqlDB, indexes); err != nil {
		return DatasetResult{}, err
	}
	if err := buildDerivedDataset(ctx, db, options); err != nil {
		return DatasetResult{}, err
	}
	if err := moveArchiveRows(db, options.ArchiveEvents); err != nil {
		return DatasetResult{}, err
	}
	if options.Vacuum {
		if err := repository.Vacuum(db); err != nil {
			return DatasetResult{}, fmt.Errorf("vacuum benchmark dataset: %w", err)
		}
	}
	if err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)").Error; err != nil {
		return DatasetResult{}, fmt.Errorf("checkpoint benchmark WAL: %w", err)
	}
	result, err := ValidateDataset(db, options.Path)
	if err != nil {
		return DatasetResult{}, err
	}
	result.GeneratorVersion = DatasetGeneratorVersion
	result.Seed = options.Seed
	result.BenchmarkNow = options.Now
	return result, nil
}

func validateGenerateOptions(options GenerateOptions) error {
	if strings.TrimSpace(options.Path) == "" {
		return fmt.Errorf("benchmark dataset path is required")
	}
	if options.HotEvents <= 0 || options.ArchiveEvents < 0 || options.HotDays <= 0 || options.ArchiveDays < 0 {
		return fmt.Errorf("benchmark event counts and day windows are invalid")
	}
	if options.FailureRate < 0 || options.FailureRate > 1 {
		return fmt.Errorf("benchmark failure rate must be between zero and one")
	}
	if options.Now.IsZero() {
		return fmt.Errorf("benchmark now is required")
	}
	if err := validateCardinality("dataset", options.Cardinality); err != nil {
		return err
	}
	if len(options.TrafficTiers) == 0 {
		return fmt.Errorf("benchmark traffic tiers are required")
	}
	if options.InsertBatchSize <= 0 {
		options.InsertBatchSize = 10_000
	}
	if options.AggregatePage <= 0 {
		options.AggregatePage = 1_000
	}
	return nil
}

func seedDatasetMetadata(db *gorm.DB, options GenerateOptions) ([]identityProfile, error) {
	now := timeutil.NormalizeStorageTime(options.Now)
	apiProfiles, err := BuildAPIKeyProfiles(options.Cardinality.APIKeys, options.TrafficTiers, options.Seed)
	if err != nil {
		return nil, err
	}
	apiKeys := make([]entities.CPAAPIKey, 0, len(apiProfiles))
	for _, profile := range apiProfiles {
		apiKeys = append(apiKeys, entities.CPAAPIKey{
			ID: int64(profile.Index), APIKey: fmt.Sprintf("bench-key-%03d", profile.Index),
			DisplayKey: fmt.Sprintf("bench-***-%03d", profile.Index), KeyAlias: fmt.Sprintf("bench-%s-%03d", profile.Tier, profile.Index),
			IsDeleted: false, LastSyncedAt: &now, CreatedAt: now, UpdatedAt: now,
		})
	}
	if err := db.CreateInBatches(apiKeys, 100).Error; err != nil {
		return nil, fmt.Errorf("seed benchmark API keys: %w", err)
	}

	identityRows := make([]entities.UsageIdentity, 0, options.Cardinality.Identities)
	identityProfiles := make([]identityProfile, 0, options.Cardinality.Identities)
	for index := 1; index <= options.Cardinality.Identities; index++ {
		authType := entities.UsageIdentityAuthTypeAuthFile
		authTypeName := "auth_file"
		provider := "codex"
		eventAuthType := "oauth"
		if index%4 == 0 {
			authType = entities.UsageIdentityAuthTypeAIProvider
			authTypeName = "ai_provider"
			provider = "openai"
			eventAuthType = "apikey"
		}
		identity := fmt.Sprintf("bench-auth-%04d", index)
		identityRows = append(identityRows, entities.UsageIdentity{
			ID: int64(index), Name: identity, AuthType: authType, AuthTypeName: authTypeName,
			Identity: identity, Type: provider, Provider: provider, LookupKey: identity,
			ActiveStart: &now, IsDeleted: false, CreatedAt: now, UpdatedAt: now,
		})
		identityProfiles = append(identityProfiles, identityProfile{Identity: identity, AuthType: authType})
		_ = eventAuthType
	}
	if err := db.CreateInBatches(identityRows, 100).Error; err != nil {
		return nil, fmt.Errorf("seed benchmark identities: %w", err)
	}

	prices := make([]entities.ModelPriceSetting, 0, options.Cardinality.Models)
	for index := 1; index <= options.Cardinality.Models; index++ {
		multiplier := 1.0
		prices = append(prices, entities.ModelPriceSetting{
			ID: int64(index), Model: fmt.Sprintf("bench-model-%03d", index), PricingStyle: entities.ModelPricingStyleOpenAI,
			PromptPricePer1M: 1 + float64(index%7)/10, CompletionPricePer1M: 4 + float64(index%11)/10,
			CacheReadPricePer1M: 0.1, CacheWritePricePer1M: 1.25, PriceMultiplier: &multiplier,
			CreatedAt: now, UpdatedAt: now,
		})
	}
	if err := db.CreateInBatches(prices, 100).Error; err != nil {
		return nil, fmt.Errorf("seed benchmark pricing: %w", err)
	}
	return identityProfiles, nil
}

func insertGeneratedEvents(ctx context.Context, sqlDB *sql.DB, options GenerateOptions, identities []identityProfile) error {
	total := options.HotEvents + options.ArchiveEvents
	apiProfiles, err := BuildAPIKeyProfiles(options.Cardinality.APIKeys, options.TrafficTiers, options.Seed)
	if err != nil {
		return err
	}
	apiTraffic, err := newAPITrafficTopology(apiProfiles)
	if err != nil {
		return err
	}
	random := splitMix64{state: options.Seed ^ 0x6a09e667f3bcc909}
	timeline := buildEventTimeline(options)
	batchSize := options.InsertBatchSize
	if batchSize <= 0 {
		batchSize = 10_000
	}

	for startID := int64(1); startID <= total; startID += int64(batchSize) {
		endID := min(startID+int64(batchSize)-1, total)
		tx, err := sqlDB.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin benchmark event batch: %w", err)
		}
		placeholders := strings.TrimSuffix(strings.Repeat("?,", 31), ",")
		statement, err := tx.PrepareContext(ctx, "INSERT INTO usage_events ("+usageEventInsertColumns+") VALUES ("+placeholders+")")
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("prepare benchmark event insert: %w", err)
		}
		for eventID := startID; eventID <= endID; eventID++ {
			timestamp, ok := timeline.next()
			if !ok {
				statement.Close()
				tx.Rollback()
				return fmt.Errorf("benchmark timeline ended before event %d", eventID)
			}
			event := makeGeneratedEvent(eventID, timestamp, options, identities, apiTraffic, &random)
			if _, err := statement.ExecContext(ctx, eventInsertArgs(event)...); err != nil {
				statement.Close()
				tx.Rollback()
				return fmt.Errorf("insert benchmark event %d: %w", eventID, err)
			}
		}
		if err := statement.Close(); err != nil {
			tx.Rollback()
			return fmt.Errorf("close benchmark event statement: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit benchmark event batch: %w", err)
		}
	}
	return nil
}

func makeGeneratedEvent(eventID int64, timestamp time.Time, options GenerateOptions, identities []identityProfile, apiTraffic apiTrafficTopology, random *splitMix64) generatedEvent {
	apiIndex := apiTraffic.choose(timestamp, random)
	identityIndex := chooseCorrelatedIdentity(apiIndex, options.Cardinality, timestamp, random)
	modelIndex := chooseCorrelatedModel(identityIndex, options.Cardinality, random)
	// 开头的覆盖段保证全部 metadata 至少被一条事件引用，后续仍完全遵循权重。
	if eventID <= int64(options.Cardinality.APIKeys) {
		apiIndex = int(eventID - 1)
		identityIndex = chooseCorrelatedIdentity(apiIndex, options.Cardinality, timestamp, random)
	}
	if eventID <= int64(options.Cardinality.Identities) {
		identityIndex = int(eventID - 1)
		apiIndex = identityIndex % options.Cardinality.APIKeys
	}
	if eventID <= int64(options.Cardinality.Models) {
		modelIndex = int(eventID - 1)
	}
	identity := identities[identityIndex]
	authType := "oauth"
	provider := "codex"
	if identity.AuthType == entities.UsageIdentityAuthTypeAIProvider {
		authType = "apikey"
		provider = "openai"
	}
	input := int64(80 + random.next()%4000)
	output := int64(20 + random.next()%1200)
	reasoning := int64(random.next() % uint64(output+1))
	cacheRead := int64(random.next() % uint64(max(input/2, 1)))
	cacheCreation := int64(random.next() % uint64(max(input/5, 1)))
	cached := cacheRead
	total := input + output
	failed := random.float64() < options.FailureRate
	latencyMS := int64(250 + random.next()%45_000)
	ttftMS := int64(50 + random.next()%min(uint64(latencyMS), uint64(8_000)))
	eventKeyIndex := eventID
	if eventID%100 == 0 {
		eventKeyIndex = eventID - 1
	}
	requestIDIndex := eventID
	if eventID%250 == 0 {
		requestIDIndex = eventID - 1
	}
	endpoint, serviceTier, responseTier, reasoningEffort, executorType := correlatedDimensions(modelIndex, identityIndex, provider, random)
	return generatedEvent{
		ID: eventID, EventKey: fmt.Sprintf("bench-event-%012d", eventKeyIndex), APIGroupKey: fmt.Sprint(apiIndex + 1),
		Provider: provider, Endpoint: endpoint, AuthType: authType,
		RequestID: fmt.Sprintf("bench-request-%012d", requestIDIndex), Model: fmt.Sprintf("bench-model-%03d", modelIndex+1),
		ModelAlias: fmt.Sprintf("bench-alias-%03d", modelIndex+1), ReasoningEffort: reasoningEffort,
		ServiceTier: serviceTier, ResponseServiceTier: responseTier, ExecutorType: executorType,
		Timestamp: timestamp, Source: provider, AuthIndex: identity.Identity, Failed: failed,
		LatencyMS: latencyMS, TTFTMS: ttftMS, InputTokens: input, OutputTokens: output, ReasoningTokens: reasoning,
		CachedTokens: cached, CacheReadTokens: cacheRead, CacheCreationTokens: cacheCreation, TotalTokens: total,
	}
}

func chooseCorrelatedIdentity(apiIndex int, cardinality Cardinality, timestamp time.Time, random *splitMix64) int {
	poolAPI := apiIndex
	// 一部分事件从相邻 Key 的身份池中选择，复刻真实场景中同一身份被多个 Key 使用，但不形成全交叉。
	if cardinality.APIKeys > 1 && random.float64() < 0.20 {
		poolAPI = (apiIndex + 1) % cardinality.APIKeys
	}
	if poolAPI >= cardinality.Identities {
		return poolAPI % cardinality.Identities
	}
	poolSize := (cardinality.Identities-1-poolAPI)/cardinality.APIKeys + 1
	unit := random.float64()
	// 十次方长尾接近样本的“小时内少量身份活跃、全周期仍覆盖全部有效身份”。
	skewed := unit
	for index := 1; index < 10; index++ {
		skewed *= unit
	}
	rank := int(skewed * float64(poolSize))
	if rank >= poolSize {
		rank = poolSize - 1
	}
	if rank > 0 && poolSize > 2 {
		tailSize := poolSize - 1
		activeTail := max(1, int(math.Ceil(float64(tailSize)*0.15)))
		week := timestamp.Unix() / int64((7 * 24 * time.Hour).Seconds())
		rotator := splitMix64{state: uint64(week) ^ uint64(poolAPI+1)*0xbf58476d1ce4e5b9}
		rotation := int(rotator.next() % uint64(tailSize))
		rank = 1 + (rotation+(rank-1)%activeTail)%tailSize
	}
	return poolAPI + rank*cardinality.APIKeys
}

func chooseCorrelatedModel(identityIndex int, cardinality Cardinality, random *splitMix64) int {
	modelCount := cardinality.Models
	clusterSize := min(modelCount, max(4, int(math.Ceil(math.Sqrt(float64(modelCount))*2))))
	primaryAPI := identityIndex % cardinality.APIKeys
	clusterStart := (primaryAPI * 7) % modelCount
	localRank := identityIndex / cardinality.APIKeys
	primaryOffset := localRank % clusterSize
	roll := random.next() % 1000
	switch {
	case roll < 850:
		return (clusterStart + primaryOffset) % modelCount
	case roll < 990:
		return (clusterStart + (primaryOffset+1+localRank%3)%clusterSize) % modelCount
	default:
		return (clusterStart + (primaryOffset+3+localRank%5)%clusterSize) % modelCount
	}
}

func correlatedDimensions(modelIndex, identityIndex int, provider string, random *splitMix64) (string, string, string, string, string) {
	endpoints := []string{"responses", "chat/completions", "messages"}
	reasoning := []string{"", "low", "medium", "high"}
	endpoint := endpoints[modelIndex%len(endpoints)]
	serviceTier := "default"
	if modelIndex%10 < 2 {
		serviceTier = ""
	} else if modelIndex%10 >= 8 {
		serviceTier = "priority"
	}
	responseTier := serviceTier
	if identityIndex%7 == 0 {
		responseTier = ""
	}
	reasoningEffort := reasoning[modelIndex%len(reasoning)]
	executorType := "native"
	if provider == "openai" {
		executorType = "compat"
	} else if identityIndex%6 == 0 {
		executorType = ""
	}
	// 仅 0.5% 事件使用同一个相关替代 tuple，避免五个维度独立随机产生笛卡尔爆炸。
	if random.next()%1000 >= 995 {
		endpoint = endpoints[(modelIndex+1)%len(endpoints)]
		if serviceTier == "priority" {
			serviceTier = "default"
		} else {
			serviceTier = "priority"
		}
		responseTier = serviceTier
		reasoningEffort = reasoning[(modelIndex+1)%len(reasoning)]
		if executorType == "native" {
			executorType = "compat"
		} else {
			executorType = "native"
		}
	}
	return endpoint, serviceTier, responseTier, reasoningEffort, executorType
}

func eventInsertArgs(event generatedEvent) []any {
	timestamp := timeutil.FormatStorageTime(event.Timestamp)
	return []any{
		event.ID, event.EventKey, event.APIGroupKey, event.Provider, event.Endpoint, event.AuthType, event.RequestID,
		nil, nil, nil, event.Model, event.ModelAlias, event.ReasoningEffort, event.ServiceTier, event.ResponseServiceTier,
		event.ExecutorType, timestamp, event.Source, event.AuthIndex, event.Failed, true, event.LatencyMS, event.TTFTMS,
		event.InputTokens, event.OutputTokens, event.ReasoningTokens, event.CachedTokens, event.CacheReadTokens,
		event.CacheCreationTokens, event.TotalTokens, timestamp,
	}
}

func buildEventTimeline(options GenerateOptions) *eventTimeline {
	now := timeutil.NormalizeStorageTime(options.Now)
	hotStart := now.AddDate(0, 0, -options.HotDays)
	archiveStart := hotStart.AddDate(0, 0, -options.ArchiveDays)
	buckets := make([]timeBucket, 0, (options.HotDays+options.ArchiveDays)*24)
	if options.ArchiveEvents > 0 && options.ArchiveDays > 0 {
		buckets = append(buckets, weightedTimeBuckets(archiveStart, options.ArchiveDays*24, options.ArchiveEvents, options.Seed^1)...)
	}
	recentDays := min(options.HotDays, 30)
	olderDays := options.HotDays - recentDays
	recentEvents := options.HotEvents
	olderEvents := int64(0)
	if olderDays > 0 {
		olderEvents = int64(math.Round(float64(options.HotEvents) * 0.37))
		recentEvents = options.HotEvents - olderEvents
		buckets = append(buckets, weightedTimeBuckets(hotStart, olderDays*24, olderEvents, options.Seed^2)...)
	}
	recentStart := hotStart.AddDate(0, 0, olderDays)
	buckets = append(buckets, weightedTimeBuckets(recentStart, recentDays*24, recentEvents, options.Seed^3)...)
	return &eventTimeline{buckets: buckets}
}

func weightedTimeBuckets(start time.Time, hours int, total int64, seed uint64) []timeBucket {
	if hours <= 0 || total <= 0 {
		return nil
	}
	hourWeights := [...]float64{0.35, 0.28, 0.25, 0.25, 0.35, 0.60, 0.90, 1.20, 1.50, 1.70, 1.80, 1.70, 1.60, 1.50, 1.50, 1.60, 1.80, 2.00, 2.20, 2.00, 1.60, 1.20, 0.80, 0.50}
	weights := make([]float64, hours)
	random := splitMix64{state: seed}
	for index := range weights {
		timestamp := start.Add(time.Duration(index) * time.Hour)
		weekday := 1.0
		if timestamp.Weekday() == time.Saturday || timestamp.Weekday() == time.Sunday {
			weekday = 0.78
		}
		burst := 1.0
		if random.next()%97 == 0 {
			burst = 3.5
		}
		weights[index] = hourWeights[timestamp.Hour()] * weekday * burst
	}
	counts := allocateWeightedInteger(total, weights)
	buckets := make([]timeBucket, 0, hours)
	for index, count := range counts {
		if count > 0 {
			buckets = append(buckets, timeBucket{start: start.Add(time.Duration(index) * time.Hour), count: count})
		}
	}
	return buckets
}

func allocateWeightedInteger(total int64, weights []float64) []int64 {
	counts := make([]int64, len(weights))
	remainders := make([]fractionalShare, len(weights))
	weightTotal := 0.0
	for _, weight := range weights {
		weightTotal += weight
	}
	allocated := int64(0)
	for index, weight := range weights {
		exact := float64(total) * weight / weightTotal
		counts[index] = int64(math.Floor(exact))
		allocated += counts[index]
		remainders[index] = fractionalShare{index: index, fraction: exact - float64(counts[index])}
	}
	sort.SliceStable(remainders, func(left, right int) bool { return remainders[left].fraction > remainders[right].fraction })
	for index := int64(0); index < total-allocated; index++ {
		counts[remainders[index%int64(len(remainders))].index]++
	}
	return counts
}

func (timeline *eventTimeline) next() (time.Time, bool) {
	for timeline.bucketIndex < len(timeline.buckets) {
		bucket := timeline.buckets[timeline.bucketIndex]
		if timeline.inside >= bucket.count {
			timeline.bucketIndex++
			timeline.inside = 0
			continue
		}
		position := timeline.inside
		timeline.inside++
		offset := time.Duration((float64(position) + 0.5) / float64(bucket.count) * float64(time.Hour))
		return bucket.start.Add(offset), true
	}
	return time.Time{}, false
}

func newWeightedSelector(weights []float64) weightedSelector {
	selector := weightedSelector{cumulative: make([]float64, len(weights))}
	for index, weight := range weights {
		selector.total += weight
		selector.cumulative[index] = selector.total
	}
	return selector
}

func (selector weightedSelector) choose(unit float64) int {
	target := unit * selector.total
	index := sort.Search(len(selector.cumulative), func(index int) bool { return selector.cumulative[index] >= target })
	if index >= len(selector.cumulative) {
		return len(selector.cumulative) - 1
	}
	return index
}

func dropSecondaryIndexes(ctx context.Context, sqlDB *sql.DB, table string) ([]storedIndex, error) {
	rows, err := sqlDB.QueryContext(ctx, "SELECT name, sql FROM sqlite_master WHERE type = 'index' AND tbl_name = ? AND sql IS NOT NULL ORDER BY name", table)
	if err != nil {
		return nil, fmt.Errorf("list benchmark indexes: %w", err)
	}
	defer rows.Close()
	var indexes []storedIndex
	for rows.Next() {
		var index storedIndex
		if err := rows.Scan(&index.Name, &index.SQL); err != nil {
			return nil, fmt.Errorf("scan benchmark index: %w", err)
		}
		indexes = append(indexes, index)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate benchmark indexes: %w", err)
	}
	for _, index := range indexes {
		if _, err := sqlDB.ExecContext(ctx, "DROP INDEX "+quoteSQLiteIdentifier(index.Name)); err != nil {
			return nil, fmt.Errorf("drop benchmark index %s: %w", index.Name, err)
		}
	}
	return indexes, nil
}

func restoreIndexes(ctx context.Context, sqlDB *sql.DB, indexes []storedIndex) error {
	for _, index := range indexes {
		if _, err := sqlDB.ExecContext(ctx, index.SQL); err != nil {
			return fmt.Errorf("restore benchmark index %s: %w", index.Name, err)
		}
	}
	return nil
}

func quoteSQLiteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func buildDerivedDataset(ctx context.Context, db *gorm.DB, options GenerateOptions) error {
	target := options.HotEvents + options.ArchiveEvents
	pageSize := options.AggregatePage
	if pageSize <= 0 {
		pageSize = 1_000
	}
	for _, kind := range []entities.UsageAggregationCheckpointName{
		entities.UsageAggregationCheckpointOverview,
		entities.UsageAggregationCheckpointActivity,
		entities.UsageAggregationCheckpointLatency,
	} {
		cursor := int64(0)
		for cursor < target {
			var events []entities.UsageEvent
			if err := db.WithContext(ctx).Model(&entities.UsageEvent{}).
				Select(entities.UsageAggregationEventProjectionColumns).
				Where("id > ? AND id <= ?", cursor, target).
				Order("id asc").Limit(pageSize).Find(&events).Error; err != nil {
				return fmt.Errorf("load %s benchmark aggregation page: %w", kind, err)
			}
			if len(events) == 0 {
				return fmt.Errorf("%s benchmark aggregation stopped at %d before %d", kind, cursor, target)
			}
			nextCursor := events[len(events)-1].ID
			switch kind {
			case entities.UsageAggregationCheckpointOverview:
				hourly, daily, _ := overview.BuildRows(events)
				if err := repository.ApplyUsageOverviewAggregationPage(ctx, db, cursor, nextCursor, hourly, daily, options.Now); err != nil {
					return fmt.Errorf("apply overview benchmark aggregation: %w", err)
				}
			case entities.UsageAggregationCheckpointActivity:
				rows, err := activity.BuildRows(events, options.Now)
				if err != nil {
					return err
				}
				if err := repository.ApplyUsageActivityAggregationPage(ctx, db, cursor, nextCursor, rows, options.Now); err != nil {
					return fmt.Errorf("apply activity benchmark aggregation: %w", err)
				}
			case entities.UsageAggregationCheckpointLatency:
				rows, err := latency.BuildRows(events, options.Now)
				if err != nil {
					return err
				}
				if err := repository.ApplyUsageLatencyAggregationPage(ctx, db, cursor, nextCursor, rows, options.Now); err != nil {
					return fmt.Errorf("apply latency benchmark aggregation: %w", err)
				}
			}
			cursor = nextCursor
		}
	}
	if err := repository.AggregateUsageIdentityStats(ctx, db, options.Now); err != nil {
		return fmt.Errorf("aggregate benchmark identities: %w", err)
	}
	return nil
}

func moveArchiveRows(db *gorm.DB, archiveEvents int64) error {
	if archiveEvents == 0 {
		return nil
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("INSERT INTO usage_events_archive ("+entities.UsageEventStorageColumns+") SELECT "+entities.UsageEventStorageColumns+" FROM usage_events WHERE id <= ? ORDER BY id", archiveEvents).Error; err != nil {
			return fmt.Errorf("copy benchmark archive rows: %w", err)
		}
		if err := tx.Where("id <= ?", archiveEvents).Delete(&entities.UsageEvent{}).Error; err != nil {
			return fmt.Errorf("delete benchmark archived hot rows: %w", err)
		}
		return nil
	})
}

func ValidateDataset(db *gorm.DB, path string) (DatasetResult, error) {
	result := DatasetResult{Path: filepath.Clean(path)}
	queries := []struct {
		name   string
		query  string
		target *int64
	}{
		{"hot events", "SELECT COUNT(*) FROM usage_events", &result.HotEvents},
		{"archive events", "SELECT COUNT(*) FROM usage_events_archive", &result.ArchiveEvents},
		{"failed events", "SELECT COUNT(*) FROM (SELECT failed FROM usage_events UNION ALL SELECT failed FROM usage_events_archive) WHERE failed", &result.FailedEvents},
		{"identities", "SELECT COUNT(*) FROM usage_identities WHERE is_deleted = 0 AND COALESCE(disabled, 0) = 0", &result.Identities},
		{"models", "SELECT COUNT(*) FROM model_price_settings", &result.Models},
		{"API keys", "SELECT COUNT(*) FROM cpa_api_keys WHERE is_deleted = 0", &result.APIKeys},
		{"used identities", "SELECT COUNT(DISTINCT auth_index) FROM (SELECT auth_index FROM usage_events UNION ALL SELECT auth_index FROM usage_events_archive)", &result.UsedIdentities},
		{"used models", "SELECT COUNT(DISTINCT model) FROM (SELECT model FROM usage_events UNION ALL SELECT model FROM usage_events_archive)", &result.UsedModels},
		{"used API keys", "SELECT COUNT(DISTINCT api_group_key) FROM (SELECT api_group_key FROM usage_events UNION ALL SELECT api_group_key FROM usage_events_archive)", &result.UsedAPIKeys},
		{"orphan identities", "SELECT COUNT(*) FROM (SELECT auth_index, auth_type FROM usage_events UNION ALL SELECT auth_index, auth_type FROM usage_events_archive) e LEFT JOIN usage_identities i ON i.identity = e.auth_index AND ((i.auth_type = 1 AND e.auth_type = 'oauth') OR (i.auth_type = 2 AND e.auth_type = 'apikey')) AND i.is_deleted = 0 WHERE i.id IS NULL", &result.OrphanIdentities},
		{"orphan models", "SELECT COUNT(*) FROM (SELECT model FROM usage_events UNION ALL SELECT model FROM usage_events_archive) e LEFT JOIN model_price_settings p ON p.model = e.model WHERE p.id IS NULL", &result.OrphanModels},
		{"orphan API keys", "SELECT COUNT(*) FROM (SELECT api_group_key FROM usage_events UNION ALL SELECT api_group_key FROM usage_events_archive) e LEFT JOIN cpa_api_keys k ON CAST(k.id AS TEXT) = e.api_group_key AND k.is_deleted = 0 WHERE k.id IS NULL", &result.OrphanAPIKeys},
		{"duplicate event keys", "SELECT COUNT(*) - COUNT(DISTINCT event_key) FROM (SELECT event_key FROM usage_events UNION ALL SELECT event_key FROM usage_events_archive)", &result.DuplicateEventKeys},
		{"overview hourly requests", "SELECT COALESCE(SUM(request_count), 0) FROM usage_overview_hourly_stats", &result.OverviewHourlyRequests},
		{"overview daily requests", "SELECT COALESCE(SUM(request_count), 0) FROM usage_overview_daily_stats", &result.OverviewDailyRequests},
		{"identity requests", "SELECT COALESCE(SUM(total_requests), 0) FROM usage_identities", &result.IdentityRequests},
		{"overview hourly rows", "SELECT COUNT(*) FROM usage_overview_hourly_stats", &result.OverviewHourlyRows},
		{"overview daily rows", "SELECT COUNT(*) FROM usage_overview_daily_stats", &result.OverviewDailyRows},
		{"activity rows", "SELECT COUNT(*) FROM usage_activity_stats", &result.ActivityRows},
		{"latency rows", "SELECT COUNT(*) FROM usage_latency_stats", &result.LatencyRows},
		{"checkpoint minimum", "SELECT COALESCE(MIN(last_aggregated_usage_event_id), 0) FROM usage_aggregation_checkpoints", &result.CheckpointMin},
		{"checkpoint maximum", "SELECT COALESCE(MAX(last_aggregated_usage_event_id), 0) FROM usage_aggregation_checkpoints", &result.CheckpointMax},
	}
	for _, item := range queries {
		if err := db.Raw(item.query).Scan(item.target).Error; err != nil {
			return DatasetResult{}, fmt.Errorf("validate benchmark %s: %w", item.name, err)
		}
	}
	result.TotalEvents = result.HotEvents + result.ArchiveEvents
	var totals struct {
		InputTokens             int64
		OutputTokens            int64
		TotalLatencyMS          int64
		TokenSemanticViolations int64
	}
	if err := db.Raw(`
		SELECT COALESCE(SUM(input_tokens), 0) AS input_tokens,
		       COALESCE(SUM(output_tokens), 0) AS output_tokens,
		       COALESCE(SUM(latency_ms), 0) AS total_latency_ms,
		       COALESCE(SUM(CASE WHEN reasoning_tokens > output_tokens
		                              OR cache_read_tokens + cache_creation_tokens > input_tokens
		                              OR cached_tokens != cache_read_tokens
		                              OR total_tokens != input_tokens + output_tokens
		                         THEN 1 ELSE 0 END), 0) AS token_semantic_violations
		FROM (
			SELECT input_tokens, output_tokens, reasoning_tokens, cached_tokens, cache_read_tokens, cache_creation_tokens, total_tokens, latency_ms FROM usage_events
			UNION ALL
			SELECT input_tokens, output_tokens, reasoning_tokens, cached_tokens, cache_read_tokens, cache_creation_tokens, total_tokens, latency_ms FROM usage_events_archive
		)`).Scan(&totals).Error; err != nil {
		return DatasetResult{}, fmt.Errorf("validate benchmark numeric totals: %w", err)
	}
	result.InputTokens = totals.InputTokens
	result.OutputTokens = totals.OutputTokens
	result.TotalLatencyMS = totals.TotalLatencyMS
	result.TokenSemanticViolations = totals.TokenSemanticViolations
	if err := db.Raw("PRAGMA quick_check").Scan(&result.QuickCheck).Error; err != nil {
		return DatasetResult{}, fmt.Errorf("quick check benchmark database: %w", err)
	}
	if info, err := os.Stat(path); err == nil {
		result.DatabaseBytes = info.Size()
	} else {
		return DatasetResult{}, fmt.Errorf("stat benchmark database: %w", err)
	}
	semantic := struct {
		Hot, Archive, Total, Failed, Identities, Models, APIKeys, UsedIdentities, UsedModels, UsedAPIKeys, DuplicateKeys  int64
		TokenSemanticViolations                                                                                           int64
		OverviewHourly, OverviewDaily, IdentityRequests, OverviewHourlyRows, OverviewDailyRows, ActivityRows, LatencyRows int64
		InputTokens, OutputTokens, TotalLatencyMS, CheckpointMin, CheckpointMax                                           int64
	}{
		result.HotEvents, result.ArchiveEvents, result.TotalEvents, result.FailedEvents,
		result.Identities, result.Models, result.APIKeys, result.UsedIdentities, result.UsedModels, result.UsedAPIKeys, result.DuplicateEventKeys,
		result.TokenSemanticViolations,
		result.OverviewHourlyRequests, result.OverviewDailyRequests, result.IdentityRequests,
		result.OverviewHourlyRows, result.OverviewDailyRows, result.ActivityRows, result.LatencyRows,
		result.InputTokens, result.OutputTokens, result.TotalLatencyMS,
		result.CheckpointMin, result.CheckpointMax,
	}
	data, err := json.Marshal(semantic)
	if err != nil {
		return DatasetResult{}, fmt.Errorf("encode benchmark semantic fingerprint: %w", err)
	}
	digest := sha256.Sum256(data)
	result.SemanticFingerprint = hex.EncodeToString(digest[:])
	return result, nil
}
