package migration

import (
	"testing"
	"time"

	"cpa-usage-keeper/internal/cpa"
	"cpa-usage-keeper/internal/entities"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const redisUsageInboxStatusPending = "pending"

func seedAIProviderAuthIndexMigrationDatabase(t *testing.T, dbPath string) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(testSQLiteDSN(dbPath)), &gorm.Config{})
	if err != nil {
		t.Fatalf("open AI provider auth-index migration database: %v", err)
	}
	defer closeOpenedDatabase(t, db)

	if err := db.Exec(`CREATE TABLE usage_identities (
		id integer PRIMARY KEY AUTOINCREMENT,
		name text,
		auth_type integer,
		auth_type_name text,
		identity text,
		type text,
		provider text,
		total_requests integer DEFAULT 0,
		success_count integer DEFAULT 0,
		failure_count integer DEFAULT 0,
		input_tokens integer DEFAULT 0,
		output_tokens integer DEFAULT 0,
		reasoning_tokens integer DEFAULT 0,
		cached_tokens integer DEFAULT 0,
		total_tokens integer DEFAULT 0,
		last_aggregated_usage_event_id integer DEFAULT 0,
		first_used_at datetime,
		last_used_at datetime,
		stats_updated_at datetime,
		is_deleted numeric DEFAULT false,
		created_at datetime,
		updated_at datetime,
		deleted_at datetime
	)`).Error; err != nil {
		t.Fatalf("create usage_identities table: %v", err)
	}
	if err := db.Exec(`CREATE UNIQUE INDEX uniq_usage_identities_type_identity ON usage_identities(auth_type, identity)`).Error; err != nil {
		t.Fatalf("create usage identity unique index: %v", err)
	}
	if err := db.Exec(`CREATE TABLE usage_events (
		id integer PRIMARY KEY AUTOINCREMENT,
		event_key text,
		api_group_key text,
		provider text,
		endpoint text,
		auth_type text,
		request_id text,
		model text,
		timestamp datetime,
		source text,
		auth_index text,
		failed numeric,
		latency_ms integer,
		input_tokens integer,
		output_tokens integer,
		reasoning_tokens integer,
		cached_tokens integer,
		total_tokens integer,
		created_at datetime
	)`).Error; err != nil {
		t.Fatalf("create usage_events table: %v", err)
	}

	now := time.Date(2026, 5, 5, 7, 0, 0, 0, time.UTC)
	identityRows := []struct {
		name         string
		authType     entities.UsageIdentityAuthType
		authTypeName string
		identity     string
		typeName     string
		provider     string
		totalTokens  int64
	}{
		{name: "Claude", authType: entities.UsageIdentityAuthTypeAIProvider, authTypeName: "apikey", identity: "sk-claude-old", typeName: "claude", provider: "Claude", totalTokens: 999},
		{name: "Gemini", authType: entities.UsageIdentityAuthTypeAIProvider, authTypeName: "apikey", identity: "sk-duplicate", typeName: "gemini", provider: "Gemini", totalTokens: 888},
		{name: "", authType: entities.UsageIdentityAuthTypeAIProvider, authTypeName: "apikey", identity: "authidx-existing", typeName: "", provider: "", totalTokens: 777},
		{name: "Free", authType: entities.UsageIdentityAuthTypeAIProvider, authTypeName: "apikey", identity: "sk-ambiguous", typeName: "openai", provider: "Free", totalTokens: 666},
		{name: "Claude", authType: entities.UsageIdentityAuthTypeAIProvider, authTypeName: "apikey", identity: "sk-provider-mismatch", typeName: "claude", provider: "Claude", totalTokens: 555},
		{name: "Claude", authType: entities.UsageIdentityAuthTypeAIProvider, authTypeName: "apikey", identity: "sk-no-events", typeName: "claude", provider: "Claude", totalTokens: 444},
		{name: "OAuth User", authType: entities.UsageIdentityAuthTypeAuthFile, authTypeName: "oauth", identity: "auth-file-index", typeName: "claude", provider: "Claude", totalTokens: 333},
		{name: "Non API Key", authType: entities.UsageIdentityAuthTypeAIProvider, authTypeName: "oauth", identity: "non-apikey-identity", typeName: "claude", provider: "Claude", totalTokens: 222},
	}
	for _, row := range identityRows {
		if err := db.Exec(`INSERT INTO usage_identities (name, auth_type, auth_type_name, identity, type, provider, total_requests, success_count, total_tokens, last_aggregated_usage_event_id, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, row.name, row.authType, row.authTypeName, row.identity, row.typeName, row.provider, 9, 9, row.totalTokens, 99, now, now).Error; err != nil {
			t.Fatalf("seed usage identity %q: %v", row.identity, err)
		}
	}

	events := []struct {
		eventKey        string
		provider        string
		authType        string
		source          string
		authIndex       string
		failed          bool
		inputTokens     int64
		outputTokens    int64
		reasoningTokens int64
		cachedTokens    int64
		totalTokens     int64
		timestamp       time.Time
	}{
		{eventKey: "claude-success", provider: "claude", authType: "apikey", source: "sk-claude-old", authIndex: "authidx-claude-1", failed: false, inputTokens: 7, outputTokens: 8, reasoningTokens: 2, cachedTokens: 3, totalTokens: 20, timestamp: time.Date(2026, 5, 5, 8, 0, 0, 0, time.UTC)},
		{eventKey: "claude-failure", provider: "CLAUDE", authType: "apikey", source: "sk-claude-old", authIndex: "authidx-claude-1", failed: true, inputTokens: 5, outputTokens: 6, reasoningTokens: 1, cachedTokens: 1, totalTokens: 13, timestamp: time.Date(2026, 5, 5, 9, 0, 0, 0, time.UTC)},
		{eventKey: "duplicate-existing", provider: "gemini", authType: "apikey", source: "sk-duplicate", authIndex: "authidx-existing", failed: false, inputTokens: 10, outputTokens: 11, totalTokens: 21, timestamp: time.Date(2026, 5, 5, 10, 0, 0, 0, time.UTC)},
		{eventKey: "ambiguous-a", provider: "free", authType: "apikey", source: "sk-ambiguous", authIndex: "authidx-ambiguous-a", failed: false, totalTokens: 31, timestamp: time.Date(2026, 5, 5, 11, 0, 0, 0, time.UTC)},
		{eventKey: "ambiguous-b", provider: "FREE", authType: "apikey", source: "sk-ambiguous", authIndex: "authidx-ambiguous-b", failed: false, totalTokens: 32, timestamp: time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)},
		{eventKey: "wrong-provider", provider: "Gemini", authType: "apikey", source: "sk-provider-mismatch", authIndex: "authidx-wrong-provider", failed: false, totalTokens: 41, timestamp: time.Date(2026, 5, 5, 13, 0, 0, 0, time.UTC)},
		{eventKey: "auth-file-source-match", provider: "Claude", authType: "apikey", source: "auth-file-index", authIndex: "authidx-should-not-touch-auth-file", failed: false, totalTokens: 51, timestamp: time.Date(2026, 5, 5, 14, 0, 0, 0, time.UTC)},
	}
	for _, event := range events {
		if err := db.Exec(`INSERT INTO usage_events (event_key, api_group_key, provider, endpoint, auth_type, request_id, model, timestamp, source, auth_index, failed, latency_ms, input_tokens, output_tokens, reasoning_tokens, cached_tokens, total_tokens, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, event.eventKey, "group", event.provider, "/v1/messages", event.authType, event.eventKey, "claude-sonnet", event.timestamp, event.source, event.authIndex, event.failed, 100, event.inputTokens, event.outputTokens, event.reasoningTokens, event.cachedTokens, event.totalTokens, event.timestamp).Error; err != nil {
			t.Fatalf("seed usage event %q: %v", event.eventKey, err)
		}
	}
}

func seedPrefixGeneratedUsageIdentities(t *testing.T, dbPath string) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(testSQLiteDSN(dbPath)), &gorm.Config{})
	if err != nil {
		t.Fatalf("open prefix identity database: %v", err)
	}
	defer closeOpenedDatabase(t, db)

	if err := db.Exec(`CREATE TABLE usage_identities (
		id integer PRIMARY KEY AUTOINCREMENT,
		name text,
		auth_type integer,
		auth_type_name text,
		identity text,
		type text,
		provider text,
		total_requests integer DEFAULT 0,
		success_count integer DEFAULT 0,
		failure_count integer DEFAULT 0,
		input_tokens integer DEFAULT 0,
		output_tokens integer DEFAULT 0,
		reasoning_tokens integer DEFAULT 0,
		cached_tokens integer DEFAULT 0,
		total_tokens integer DEFAULT 0,
		last_aggregated_usage_event_id integer DEFAULT 0,
		first_used_at datetime,
		last_used_at datetime,
		stats_updated_at datetime,
		is_deleted numeric DEFAULT false,
		created_at datetime,
		updated_at datetime,
		deleted_at datetime
	)`).Error; err != nil {
		t.Fatalf("create usage_identities table: %v", err)
	}
	if err := db.Exec(`CREATE UNIQUE INDEX uniq_usage_identities_type_identity ON usage_identities(auth_type, identity)`).Error; err != nil {
		t.Fatalf("create usage identity unique index: %v", err)
	}
	if err := db.Exec(`CREATE TABLE usage_events (
		id integer PRIMARY KEY AUTOINCREMENT,
		event_key text,
		api_group_key text,
		provider text,
		endpoint text,
		auth_type text,
		request_id text,
		model text,
		timestamp datetime,
		source text,
		auth_index text,
		failed numeric,
		latency_ms integer,
		input_tokens integer,
		output_tokens integer,
		reasoning_tokens integer,
		cached_tokens integer,
		total_tokens integer,
		created_at datetime
	)`).Error; err != nil {
		t.Fatalf("create usage_events table: %v", err)
	}

	now := time.Date(2026, 5, 4, 8, 0, 0, 0, time.UTC)
	rows := []entities.UsageIdentity{
		{Name: "Claude Team", AuthType: entities.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "claude-key", Type: "claude", Provider: "Claude Team", TotalRequests: 1, SuccessCount: 1, TotalTokens: 30, LastAggregatedUsageEventID: 1, CreatedAt: now, UpdatedAt: now},
		{Name: "Claude Team", AuthType: entities.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "claude-unused-key", Type: "claude", Provider: "Claude Team", CreatedAt: now, UpdatedAt: now},
		{Name: "Gemini Team", AuthType: entities.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "gemini", Type: "gemini", Provider: "Gemini Team", TotalRequests: 2, SuccessCount: 2, TotalTokens: 40, LastAggregatedUsageEventID: 2, CreatedAt: now, UpdatedAt: now},
		{Name: "Claude Team", AuthType: entities.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "claude", Type: "claude", Provider: "Claude Team", CreatedAt: now, UpdatedAt: now},
		{Name: "Codex Team", AuthType: entities.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "codex", Type: "codex", Provider: "Codex Team", CreatedAt: now, UpdatedAt: now},
		{Name: "Vertex Team", AuthType: entities.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "vertex", Type: "vertex", Provider: "Vertex Team", CreatedAt: now, UpdatedAt: now},
		{Name: "OpenAI Team", AuthType: entities.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "openai", Type: "openai", Provider: "OpenAI Team", CreatedAt: now, UpdatedAt: now},
		{Name: "Gemini Team", AuthType: entities.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "gemini-unused-key", Type: "gemini", Provider: "Gemini Team", CreatedAt: now, UpdatedAt: now},
		{Name: "Custom OpenAI", AuthType: entities.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "https://proxy.internal/v1", Type: "openai", Provider: "Custom OpenAI", CreatedAt: now, UpdatedAt: now},
	}
	for _, row := range rows {
		if err := db.Exec(`INSERT INTO usage_identities (name, auth_type, auth_type_name, identity, type, provider, total_requests, success_count, total_tokens, last_aggregated_usage_event_id, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, row.Name, row.AuthType, row.AuthTypeName, row.Identity, row.Type, row.Provider, row.TotalRequests, row.SuccessCount, row.TotalTokens, row.LastAggregatedUsageEventID, row.CreatedAt, row.UpdatedAt).Error; err != nil {
			t.Fatalf("seed usage identity %q: %v", row.Identity, err)
		}
	}
	if err := db.Exec(`INSERT INTO usage_events (event_key, api_group_key, provider, endpoint, auth_type, request_id, model, timestamp, source, auth_index, failed, latency_ms, input_tokens, output_tokens, total_tokens, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, "claude-event", "group", "Claude Team", "/v1/messages", "apikey", "req", "claude-sonnet", now, "claude-key", "claude-auth-index", false, 100, 10, 20, 30, now).Error; err != nil {
		t.Fatalf("seed usage event: %v", err)
	}
}

func seedLegacyUsageIdentityTables(t *testing.T, dbPath string) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(testSQLiteDSN(dbPath)), &gorm.Config{})
	if err != nil {
		t.Fatalf("open legacy identity database: %v", err)
	}
	defer closeOpenedDatabase(t, db)

	statements := []string{
		`CREATE TABLE auth_files (
			id integer PRIMARY KEY AUTOINCREMENT,
			auth_index text,
			name text,
			email text,
			type text,
			provider text,
			label text,
			created_at datetime,
			updated_at datetime,
			deleted_at datetime
		)`,
		`CREATE TABLE provider_metadata (
			id integer PRIMARY KEY AUTOINCREMENT,
			lookup_key text,
			provider_type text,
			display_name text,
			created_at datetime,
			updated_at datetime,
			deleted_at datetime
		)`,
		`CREATE TABLE usage_events (
			id integer PRIMARY KEY AUTOINCREMENT,
			event_key text,
			api_group_key text,
			provider text,
			endpoint text,
			auth_type text,
			request_id text,
			model text,
			timestamp datetime,
			source text,
			auth_index text,
			failed numeric,
			latency_ms integer,
			input_tokens integer,
			output_tokens integer,
			reasoning_tokens integer,
			cached_tokens integer,
			total_tokens integer,
			created_at datetime
		)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("seed legacy identity schema with %q: %v", statement, err)
		}
	}

	now := time.Date(2026, 5, 4, 7, 0, 0, 0, time.UTC)
	deletedAt := time.Date(2026, 5, 4, 7, 30, 0, 0, time.UTC)
	if err := db.Exec("INSERT INTO auth_files (auth_index, name, email, type, provider, label, created_at, updated_at, deleted_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)", "auth-1", "OAuth Name", "person@example.com", "claude", "claude", "OAuth Label", now, now, nil).Error; err != nil {
		t.Fatalf("seed active auth file: %v", err)
	}
	if err := db.Exec("INSERT INTO auth_files (auth_index, name, email, type, provider, label, created_at, updated_at, deleted_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)", "auth-deleted", "Deleted OAuth", "deleted@example.com", "claude", "claude", "Deleted", now, now, deletedAt).Error; err != nil {
		t.Fatalf("seed deleted auth file: %v", err)
	}
	if err := db.Exec("INSERT INTO provider_metadata (lookup_key, provider_type, display_name, created_at, updated_at, deleted_at) VALUES (?, ?, ?, ?, ?, ?)", "api-source-1", "claude", "Claude API", now, now, nil).Error; err != nil {
		t.Fatalf("seed active provider metadata: %v", err)
	}
	if err := db.Exec("INSERT INTO provider_metadata (lookup_key, provider_type, display_name, created_at, updated_at, deleted_at) VALUES (?, ?, ?, ?, ?, ?)", "api-deleted", "claude", "Deleted API", now, now, deletedAt).Error; err != nil {
		t.Fatalf("seed deleted provider metadata: %v", err)
	}

	events := []struct {
		eventKey        string
		authType        string
		authIndex       string
		source          string
		failed          bool
		inputTokens     int64
		outputTokens    int64
		reasoningTokens int64
		cachedTokens    int64
		totalTokens     int64
		timestamp       time.Time
	}{
		{eventKey: "oauth-success", authType: "oauth", authIndex: "auth-1", failed: false, inputTokens: 10, outputTokens: 20, reasoningTokens: 3, cachedTokens: 4, totalTokens: 37, timestamp: time.Date(2026, 5, 4, 8, 0, 0, 0, time.UTC)},
		{eventKey: "legacy-oauth", authIndex: "auth-1", failed: false, inputTokens: 1, outputTokens: 1, reasoningTokens: 1, cachedTokens: 1, totalTokens: 4, timestamp: time.Date(2026, 5, 4, 8, 30, 0, 0, time.UTC)},
		{eventKey: "oauth-failure", authType: "oauth", authIndex: "auth-1", failed: true, inputTokens: 20, outputTokens: 20, reasoningTokens: 7, cachedTokens: 2, totalTokens: 49, timestamp: time.Date(2026, 5, 4, 9, 0, 0, 0, time.UTC)},
		{eventKey: "apikey-success", authType: "apikey", source: "api-source-1", failed: false, inputTokens: 7, outputTokens: 8, reasoningTokens: 9, cachedTokens: 10, totalTokens: 34, timestamp: time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC)},
		{eventKey: "legacy-apikey", source: "api-source-1", failed: false, inputTokens: 2, outputTokens: 1, reasoningTokens: 1, cachedTokens: 1, totalTokens: 5, timestamp: time.Date(2026, 5, 4, 10, 30, 0, 0, time.UTC)},
		{eventKey: "deleted-oauth", authType: "oauth", authIndex: "auth-deleted", failed: false, totalTokens: 100, timestamp: time.Date(2026, 5, 4, 11, 0, 0, 0, time.UTC)},
		{eventKey: "deleted-api", authType: "apikey", source: "api-deleted", failed: false, totalTokens: 100, timestamp: time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)},
	}
	for _, event := range events {
		if err := db.Exec(
			`INSERT INTO usage_events (event_key, api_group_key, provider, endpoint, auth_type, request_id, model, timestamp, source, auth_index, failed, latency_ms, input_tokens, output_tokens, reasoning_tokens, cached_tokens, total_tokens, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			event.eventKey, "group", "claude", "/v1/messages", event.authType, event.eventKey, "claude-sonnet", event.timestamp, event.source, event.authIndex, event.failed, 100, event.inputTokens, event.outputTokens, event.reasoningTokens, event.cachedTokens, event.totalTokens, event.timestamp,
		).Error; err != nil {
			t.Fatalf("seed usage event %s: %v", event.eventKey, err)
		}
	}
}

func seedPerformanceIndexMigrationDatabase(t *testing.T, dbPath string) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(testSQLiteDSN(dbPath)), &gorm.Config{})
	if err != nil {
		t.Fatalf("open performance index migration database: %v", err)
	}
	defer closeOpenedDatabase(t, db)

	statements := []string{
		`CREATE TABLE usage_events (
			id integer PRIMARY KEY AUTOINCREMENT,
			event_key text,
			api_group_key text,
			provider text,
			endpoint text,
			auth_type text,
			request_id text,
			model text,
			timestamp datetime,
			source text,
			auth_index text,
			failed numeric,
			latency_ms integer,
			input_tokens integer,
			output_tokens integer,
			reasoning_tokens integer,
			cached_tokens integer,
			total_tokens integer,
			created_at datetime
		)`,
		`CREATE UNIQUE INDEX uniq_usage_events_event_key ON usage_events(event_key)`,
		`CREATE INDEX idx_usage_events_timestamp ON usage_events(timestamp)`,
		`CREATE INDEX idx_usage_events_api_group_key ON usage_events(api_group_key)`,
		`CREATE INDEX idx_usage_events_source ON usage_events(source)`,
		`CREATE INDEX idx_usage_events_auth_index ON usage_events(auth_index)`,
		`CREATE TABLE redis_usage_inboxes (
			id integer PRIMARY KEY AUTOINCREMENT,
			queue_key text NOT NULL,
			message_hash text NOT NULL,
			raw_message text NOT NULL,
			status text NOT NULL,
			attempt_count integer NOT NULL DEFAULT 0,
			last_error text,
			usage_event_key text,
			popped_at datetime NOT NULL,
			processed_at datetime,
			created_at datetime,
			updated_at datetime
		)`,
		`CREATE INDEX idx_redis_usage_inboxes_status ON redis_usage_inboxes(status)`,
		`CREATE INDEX idx_redis_usage_inboxes_queue_key ON redis_usage_inboxes(queue_key)`,
		`CREATE INDEX idx_redis_usage_inboxes_message_hash ON redis_usage_inboxes(message_hash)`,
		`CREATE INDEX idx_redis_usage_inboxes_usage_event_key ON redis_usage_inboxes(usage_event_key)`,
		`CREATE INDEX idx_redis_usage_inboxes_popped_at ON redis_usage_inboxes(popped_at)`,
		`CREATE TABLE usage_identities (
			id integer PRIMARY KEY AUTOINCREMENT,
			name text,
			auth_type integer,
			auth_type_name text,
			identity text,
			type text,
			provider text,
			lookup_key text,
			total_requests integer DEFAULT 0,
			success_count integer DEFAULT 0,
			failure_count integer DEFAULT 0,
			input_tokens integer DEFAULT 0,
			output_tokens integer DEFAULT 0,
			reasoning_tokens integer DEFAULT 0,
			cached_tokens integer DEFAULT 0,
			total_tokens integer DEFAULT 0,
			last_aggregated_usage_event_id integer DEFAULT 0,
			first_used_at datetime,
			last_used_at datetime,
			stats_updated_at datetime,
			is_deleted numeric DEFAULT false,
			created_at datetime,
			updated_at datetime,
			deleted_at datetime
		)`,
		`CREATE UNIQUE INDEX uniq_usage_identities_type_identity ON usage_identities(auth_type, identity)`,
		`CREATE INDEX idx_usage_identities_auth_type ON usage_identities(auth_type)`,
		`CREATE INDEX idx_usage_identities_auth_type_name ON usage_identities(auth_type_name)`,
		`CREATE INDEX idx_usage_identities_identity ON usage_identities(identity)`,
		`CREATE INDEX idx_usage_identities_is_deleted ON usage_identities(is_deleted)`,
		`CREATE INDEX idx_usage_identities_last_aggregated_usage_event_id ON usage_identities(last_aggregated_usage_event_id)`,
		`CREATE INDEX idx_usage_identities_deleted_at ON usage_identities(deleted_at)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("seed performance index schema with %q: %v", statement, err)
		}
	}
}

func seedLegacyRedisUsageTables(t *testing.T, dbPath string) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(testSQLiteDSN(dbPath)), &gorm.Config{})
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	defer closeOpenedDatabase(t, db)

	statements := []string{
		`CREATE TABLE usage_events (
			id integer PRIMARY KEY AUTOINCREMENT,
			event_key text,
			snapshot_run_id integer,
			api_group_key text,
			model text,
			timestamp datetime,
			source text,
			auth_index text,
			failed numeric,
			latency_ms integer,
			input_tokens integer,
			output_tokens integer,
			reasoning_tokens integer,
			cached_tokens integer,
			total_tokens integer,
			created_at datetime
		)`,
		`CREATE UNIQUE INDEX uniq_usage_events_event_key ON usage_events(event_key)`,
		`CREATE TABLE redis_usage_inboxes (
			id integer PRIMARY KEY AUTOINCREMENT,
			queue_key text NOT NULL DEFAULT '',
			message_hash text NOT NULL DEFAULT '',
			raw_message text NOT NULL DEFAULT '',
			status text NOT NULL DEFAULT '',
			attempt_count integer NOT NULL DEFAULT 0,
			last_error text,
			snapshot_run_id integer,
			usage_event_key text,
			popped_at datetime NOT NULL DEFAULT '1970-01-01 00:00:00',
			processed_at datetime,
			created_at datetime,
			updated_at datetime
		)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("seed legacy schema with %q: %v", statement, err)
		}
	}

	now := time.Date(2026, 5, 3, 8, 0, 0, 0, time.UTC)
	legacyEvents := []map[string]any{
		{"event_key": "legacy-canonical-key", "api_group_key": "raw-key", "model": "claude-sonnet", "timestamp": now, "created_at": now},
		{"event_key": "req-fallback", "api_group_key": "fallback", "model": "claude-opus", "timestamp": now, "created_at": now},
		{"event_key": "req-blank-fallback", "api_group_key": "blank", "model": "claude-opus", "timestamp": now, "created_at": now},
		{"event_key": "", "api_group_key": "empty", "model": "claude-empty", "timestamp": now, "created_at": now},
		{"event_key": "existing-key", "api_group_key": "existing", "model": "claude-haiku", "timestamp": now, "created_at": now},
	}
	for _, values := range legacyEvents {
		if err := db.Table("usage_events").Create(values).Error; err != nil {
			t.Fatalf("seed legacy usage event: %v", err)
		}
	}

	inboxes := []struct {
		hash          string
		rawMessage    string
		status        string
		usageEventKey string
		processedAt   *time.Time
	}{
		{hash: "hash-1", rawMessage: `{"provider":" claude ","endpoint":" /v1/messages ","auth_type":" API_KEY ","request_id":" req-from-raw "}`, status: redisUsageInboxStatusProcessed, usageEventKey: "legacy-canonical-key", processedAt: &now},
		{hash: "hash-2", rawMessage: `{"provider":" fallback-provider ","endpoint":" /fallback ","auth_type":" OAuth ","request_id":" req-fallback "}`, status: redisUsageInboxStatusProcessed, usageEventKey: "missing-key", processedAt: &now},
		{hash: "hash-3", rawMessage: `{"provider":" overwrite-provider ","endpoint":" /overwrite ","auth_type":" api_key ","request_id":" overwrite-request "}`, status: redisUsageInboxStatusProcessed, usageEventKey: "existing-key", processedAt: &now},
		{hash: "hash-4", rawMessage: `{"provider":" blank-provider ","endpoint":" /blank ","auth_type":" OAuth ","request_id":" req-blank-fallback "}`, status: redisUsageInboxStatusProcessed, usageEventKey: "", processedAt: &now},
		{hash: "hash-5", rawMessage: `{"provider":"pending-provider","request_id":"pending-key"}`, status: redisUsageInboxStatusPending, usageEventKey: "pending-key"},
	}
	for _, inbox := range inboxes {
		if err := db.Exec(
			"INSERT INTO redis_usage_inboxes (queue_key, message_hash, raw_message, status, attempt_count, usage_event_key, popped_at, processed_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			cpa.ManagementUsageQueueKey, inbox.hash, inbox.rawMessage, inbox.status, 0, inbox.usageEventKey, now, inbox.processedAt, now, now,
		).Error; err != nil {
			t.Fatalf("seed legacy redis inbox: %v", err)
		}
	}
}
