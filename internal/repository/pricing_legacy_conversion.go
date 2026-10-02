package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/gorm"
)

const pricingLegacyVerifyPageSize = 500

var pricingLegacyColumnName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

var pricingLegacyEventConversions = map[string]bool{
	"provider": true, "endpoint": true, "auth_type": true, "request_id": true,
	"timestamp": true, "created_at": true,
	"input_tokens": true, "output_tokens": true, "cached_tokens": true,
	"cache_read_tokens": true, "total_tokens": true,
}

// VerifyPublishedPricingMigration 在 M2 末尾只读核对旧版本的转换结果。
// 调用方先经 ProtectPricingLegacyMigration 验证原备份及基线；这里不重复扫描备份总量。
// 原表字段按 ID 核对；M1 已核对旧分组，这里核对新水位及重建分组。
func VerifyPublishedPricingMigration(ctx context.Context, backupDB, liveDB *gorm.DB, original PricingLegacyBaseline) error {
	if backupDB == nil || liveDB == nil {
		return fmt.Errorf("pricing M2 verification database is missing")
	}
	if original.SchemaVersion != pricingLegacyBaselineSchemaVersion {
		return fmt.Errorf("pricing M2 original baseline version is invalid")
	}
	for _, table := range []string{"usage_events", "usage_events_archive"} {
		if err := verifyPricingM2EventTable(ctx, backupDB, liveDB, original, table); err != nil {
			return err
		}
	}
	// M1 已在破坏性旧迁移前证明原分组；这里只证明转换后新 C 内全部明细与新分组对应。
	liveSchema, err := pricingM2Schema(ctx, liveDB)
	if err != nil {
		return err
	}
	liveCursor, _, err := pricingLegacyOverviewCursor(ctx, liveDB, liveSchema)
	if err != nil {
		return fmt.Errorf("read converted overview cursor: %w", err)
	}
	if liveCursor < 0 {
		return fmt.Errorf("converted overview cursor is negative")
	}
	if pricingM2MigrationPending(original, "20260723_usage_overview_five_dimensions") {
		if liveCursor != original.Hot.MaxID {
			return fmt.Errorf("converted overview cursor %d does not match replay target %d", liveCursor, original.Hot.MaxID)
		}
	} else if liveCursor != original.Overview.Cursor {
		return fmt.Errorf("converted overview cursor changed from %d to %d without five-dimension replay", original.Overview.Cursor, liveCursor)
	}
	for _, period := range []string{"hourly", "daily"} {
		table := "usage_overview_" + period + "_stats"
		if err := verifyPricingM2Rollup(ctx, liveDB, liveSchema, table, liveCursor); err != nil {
			return fmt.Errorf("verify converted %s: %w", table, err)
		}
	}
	return nil
}

func pricingM2MigrationPending(b PricingLegacyBaseline, version string) bool {
	for _, applied := range b.SchemaMigrations {
		if applied == version {
			return false
		}
	}
	return true
}

func pricingM2Schema(ctx context.Context, db *gorm.DB) (PricingLegacyBaseline, error) {
	b := PricingLegacyBaseline{SchemaColumns: map[string][]string{}}
	for _, table := range []string{"usage_events", "usage_events_archive", "usage_overview_hourly_stats", "usage_overview_daily_stats", "usage_aggregation_checkpoints", "usage_overview_aggregation_checkpoints"} {
		if !db.Migrator().HasTable(table) {
			continue
		}
		cols, err := db.WithContext(ctx).Migrator().ColumnTypes(table)
		if err != nil {
			return b, fmt.Errorf("inspect converted %s: %w", table, err)
		}
		for _, col := range cols {
			b.SchemaColumns[table] = append(b.SchemaColumns[table], col.Name())
		}
	}
	return b, nil
}

type pricingM2EventRow struct {
	ID     int64
	Values map[string]string
}

// verifyPricingM2EventTable 按旧物理列逐 ID 对照冷热事件；已发布删除列单独豁免，新增费用列不参与。
func verifyPricingM2EventTable(ctx context.Context, backupDB, liveDB *gorm.DB, b PricingLegacyBaseline, table string) error {
	if !hasPricingBaselineTable(b, table) {
		// 归档表在较新旧版本才出现；旧库没有时，新建表必须仍为空。
		if liveDB.Migrator().HasTable(table) {
			var count int64
			if err := liveDB.WithContext(ctx).Table(table).Count(&count).Error; err != nil {
				return err
			}
			if count != 0 {
				return fmt.Errorf("%s acquired %d unrecorded events", table, count)
			}
		}
		return nil
	}
	oldCols := make([]string, 0, len(b.SchemaColumns[table]))
	for _, col := range b.SchemaColumns[table] {
		if table == "usage_events" && col == "snapshot_run_id" && pricingM2MigrationPending(b, "20260504_drop_legacy_snapshot_run_columns") {
			// 此列由已发布清理迁移明确删除，旧事件 ID 和其余原始事实仍逐项核对。
			continue
		}
		if !pricingLegacyColumnName.MatchString(col) || !liveDB.Migrator().HasColumn(table, col) {
			return fmt.Errorf("original %s.%s is missing or unsupported after migration", table, col)
		}
		oldCols = append(oldCols, col)
	}
	liveCols := append([]string(nil), oldCols...)
	if table == "usage_events" {
		for _, field := range []string{"input_tokens", "output_tokens", "reasoning_tokens", "cached_tokens", "cache_read_tokens", "cache_creation_tokens", "total_tokens", "provider", "endpoint", "auth_type", "request_id", "event_key", "source", "auth_index"} {
			if !hasPricingBaselineColumn(b, table, field) {
				if !liveDB.Migrator().HasColumn(table, field) {
					return fmt.Errorf("converted %s.%s is missing", table, field)
				}
				liveCols = append(liveCols, field)
			}
		}
	}
	var cursor int64
	var seen int64
	for {
		oldRows, err := pricingM2EventPage(ctx, backupDB, table, oldCols, cursor)
		if err != nil {
			return err
		}
		newRows, err := pricingM2EventPage(ctx, liveDB, table, liveCols, cursor)
		if err != nil {
			return err
		}
		if len(oldRows) != len(newRows) {
			return fmt.Errorf("%s event page after ID %d changed size from %d to %d", table, cursor, len(oldRows), len(newRows))
		}
		if len(oldRows) == 0 {
			break
		}
		var identities map[string]string
		if table == "usage_events" && (pricingM2MigrationPending(b, "20260601_backfill_claude_usage_tokens") || pricingM2MigrationPending(b, "20260605_backfill_gemini_codex_token_format")) {
			identities, err = pricingM2IdentityTypes(ctx, liveDB, newRows)
			if err != nil {
				return fmt.Errorf("load event identity types: %w", err)
			}
		}
		var metadata map[int64]map[string]string
		if table == "usage_events" {
			metadata, err = pricingM2MetadataPage(ctx, backupDB, liveDB, b, oldRows)
			if err != nil {
				return fmt.Errorf("verify %s metadata page after ID %d: %w", table, cursor, err)
			}
		}
		for i := range oldRows {
			for _, field := range liveCols {
				if _, exists := oldRows[i].Values[field]; !exists && field != "id" && (strings.HasSuffix(field, "_tokens") || field == "total_tokens") {
					oldRows[i].Values[field] = "0"
				}
			}
			if oldRows[i].ID != newRows[i].ID {
				return fmt.Errorf("%s event ID changed from %d to %d", table, oldRows[i].ID, newRows[i].ID)
			}
			if err := verifyPricingM2Event(b, table, oldRows[i], newRows[i], identities, metadata[oldRows[i].ID]); err != nil {
				return err
			}
		}
		seen += int64(len(oldRows))
		cursor = oldRows[len(oldRows)-1].ID
	}
	want := b.Hot.Count
	if table == "usage_events_archive" {
		want = b.Archive.Count
	}
	if seen != want {
		return fmt.Errorf("%s original event count changed from %d to %d", table, want, seen)
	}
	return nil
}

// pricingM2EventPage 只取固定数量的原始列，SQLite quote 保留 NULL、文本与整数的区别。
func pricingM2EventPage(ctx context.Context, db *gorm.DB, table string, cols []string, cursor int64) ([]pricingM2EventRow, error) {
	selects := []string{"id"}
	for _, col := range cols {
		if col != "id" {
			selects = append(selects, "quote(\""+col+"\")")
		}
	}
	rows, err := db.WithContext(ctx).Raw("SELECT "+strings.Join(selects, ", ")+" FROM "+table+" WHERE id > ? ORDER BY id LIMIT ?", cursor, pricingLegacyVerifyPageSize).Rows()
	if err != nil {
		return nil, fmt.Errorf("read %s event page: %w", table, err)
	}
	defer rows.Close()
	result := make([]pricingM2EventRow, 0, pricingLegacyVerifyPageSize)
	for rows.Next() {
		var id int64
		values := make([]string, len(selects)-1)
		dest := make([]any, len(selects))
		dest[0] = &id
		for i := range values {
			dest[i+1] = &values[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, fmt.Errorf("scan %s event: %w", table, err)
		}
		entry := pricingM2EventRow{ID: id, Values: make(map[string]string, len(cols))}
		index := 0
		for _, col := range cols {
			if col == "id" {
				continue
			}
			entry.Values[col] = values[index]
			index++
		}
		result = append(result, entry)
	}
	return result, rows.Err()
}

func pricingM2Text(value string) string {
	if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
		return strings.ReplaceAll(value[1:len(value)-1], "''", "'")
	}
	return value
}

func pricingM2NullableText(value string) string {
	if value == "NULL" {
		return ""
	}
	return pricingM2Text(value)
}

func pricingM2Int(values map[string]string, field string) (int64, error) {
	value := values[field]
	if value == "" || value == "NULL" {
		return 0, nil
	}
	parsed, err := strconv.ParseInt(pricingM2Text(value), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid %s integer %q: %w", field, value, err)
	}
	return parsed, nil
}

func verifyPricingM2Event(b PricingLegacyBaseline, table string, old, now pricingM2EventRow, identities map[string]string, metadata map[string]string) error {
	var expected map[string]int64
	if table == "usage_events" {
		if metadata != nil {
			for _, field := range pricingM2MetadataFields {
				if now.Values[field] != metadata[field] {
					return fmt.Errorf("%s event %d %s did not match published metadata conversion", table, old.ID, field)
				}
			}
		}
		var err error
		expected, err = pricingM2ExpectedTokens(b, old, now, identities)
		if err != nil {
			return fmt.Errorf("%s event %d token conversion: %w", table, old.ID, err)
		}
		// 预期转换即使未发生也要报错，不能只检查观察到差异的行。
		for _, field := range []string{"input_tokens", "output_tokens", "cached_tokens", "cache_read_tokens", "total_tokens"} {
			got, err := pricingM2Int(now.Values, field)
			if err != nil {
				return fmt.Errorf("%s event %d %s: %w", table, old.ID, field, err)
			}
			if got != expected[field] {
				return fmt.Errorf("%s event %d %s did not match published token conversion", table, old.ID, field)
			}
		}
	}
	for field, oldValue := range old.Values {
		newValue := now.Values[field]
		// 已发布迁移只把冷热表的空字符串改为 NULL；既检查正确转换，也拒绝漏转或改写真实父会话。
		if field == "parent_session_id" && pricingM2MigrationPending(b, "20260922_normalize_usage_event_parent_session_null") {
			expectedValue := oldValue
			if oldValue == "''" {
				expectedValue = "NULL"
			}
			if newValue != expectedValue {
				return fmt.Errorf("%s event %d parent_session_id did not match published null normalization", table, old.ID)
			}
			continue
		}
		if oldValue == newValue {
			continue
		}
		if table == "usage_events" && pricingLegacyEventConversions[field] {
			var allowed bool
			var err error
			switch field {
			case "timestamp", "created_at":
				allowed, err = pricingM2TimeAllowed(b, field, oldValue, newValue)
			case "provider", "endpoint", "auth_type", "request_id":
				allowed = metadata != nil && newValue == metadata[field]
			default:
				got, parseErr := pricingM2Int(now.Values, field)
				err = parseErr
				allowed = err == nil && got == expected[field]
			}
			if err != nil {
				return fmt.Errorf("%s event %d %s: %w", table, old.ID, field, err)
			}
			if allowed {
				continue
			}
		}
		return fmt.Errorf("%s event %d original %s changed unexpectedly", table, old.ID, field)
	}
	return nil
}

func pricingM2TimeAllowed(b PricingLegacyBaseline, field, old, now string) (bool, error) {
	if !pricingM2MigrationPending(b, "20260512_normalize_storage_times_to_project_tz") || old == "NULL" {
		return false, nil
	}
	parsed, err := timeutil.ParseStorageTime(pricingM2Text(old))
	if err != nil {
		return false, err
	}
	return pricingM2Text(now) == timeutil.FormatStorageTime(parsed), nil
}

// pricingM2ExpectedTokens 只执行原库尚未应用的三次已发布 Token 修正条件，预期值每事件算一次。
func pricingM2ExpectedTokens(b PricingLegacyBaseline, old, now pricingM2EventRow, identities map[string]string) (map[string]int64, error) {
	input, err := pricingM2Int(old.Values, "input_tokens")
	if err != nil {
		return nil, err
	}
	output, err := pricingM2Int(old.Values, "output_tokens")
	if err != nil {
		return nil, err
	}
	reasoning, err := pricingM2Int(old.Values, "reasoning_tokens")
	if err != nil {
		return nil, err
	}
	cached, err := pricingM2Int(old.Values, "cached_tokens")
	if err != nil {
		return nil, err
	}
	read, err := pricingM2Int(old.Values, "cache_read_tokens")
	if err != nil {
		return nil, err
	}
	creation, err := pricingM2Int(old.Values, "cache_creation_tokens")
	if err != nil {
		return nil, err
	}
	total, err := pricingM2Int(old.Values, "total_tokens")
	if err != nil {
		return nil, err
	}
	if pricingM2MigrationPending(b, "20260601_backfill_claude_usage_tokens") && read+creation > 0 {
		claude := pricingM2ProviderMatch(now, identities, []string{"claude", "anthropic"}, true)
		if claude {
			candidateInput := input
			if total == 0 {
				candidateInput = input + read + creation
			} else if total > output+reasoning {
				candidateInput = total - output - reasoning
			}
			if candidateInput > input {
				input = candidateInput
			}
			cached = read
			if total == 0 {
				total = input + output + reasoning
			}
		}
	}
	if pricingM2MigrationPending(b, "20260605_backfill_gemini_codex_token_format") && reasoning > 0 && total > 0 && input+output != total && input+output+reasoning == total {
		gemini := pricingM2ProviderMatch(now, identities, []string{"gemini", "vertex", "gemini-cli", "gemini-cli-code-assist", "antigravity", "aistudio", "ai-studio"}, false)
		if gemini {
			output += reasoning
		}
	}
	if pricingM2MigrationPending(b, "20260710_backfill_cache_read_tokens") && read <= 0 {
		read = cached
	}
	return map[string]int64{"input_tokens": input, "output_tokens": output, "cached_tokens": cached, "cache_read_tokens": read, "total_tokens": total}, nil
}

// pricingM2ProviderMatch 保持 Claude 与 Gemini 旧 SQL 各自的 auth_type、trim 和 identity fallback 规则。
func pricingM2ProviderMatch(row pricingM2EventRow, identities map[string]string, family []string, claude bool) bool {
	authType := pricingM2NullableText(row.Values["auth_type"])
	authIndex := pricingM2NullableText(row.Values["auth_index"])
	provider := pricingM2NullableText(row.Values["provider"])
	inFamily := func(value string) bool {
		for _, candidate := range family {
			if value == candidate {
				return true
			}
		}
		return false
	}
	if claude && authType == "oauth" {
		return inFamily(strings.ToLower(strings.TrimSpace(provider)))
	}
	if claude {
		if authType != "apikey" && authType != "api_key" {
			return false
		}
		identity := identities["2|"+strings.TrimSpace(authIndex)]
		return inFamily(strings.ToLower(strings.TrimSpace(identity)))
	}
	identityKey := ""
	if authType == "oauth" {
		identityKey = "1|" + authIndex
	}
	if authType == "apikey" {
		identityKey = "2|" + authIndex
	}
	if identityKey != "" {
		if identity, found := identities[identityKey]; found {
			return inFamily(strings.ToLower(identity))
		}
	}
	return inFamily(strings.ToLower(provider))
}

// pricingM2IdentityTypes 每页批量读取候选身份；同一事件的五个 Token 字段共用一次分类。
func pricingM2IdentityTypes(ctx context.Context, db *gorm.DB, events []pricingM2EventRow) (map[string]string, error) {
	wanted := map[string]bool{}
	for _, event := range events {
		index := pricingM2NullableText(event.Values["auth_index"])
		wanted[index] = true
		wanted[strings.TrimSpace(index)] = true
	}
	values := make([]string, 0, len(wanted))
	for value := range wanted {
		values = append(values, value)
	}
	result := map[string]string{}
	for offset := 0; offset < len(values); offset += 200 {
		end := offset + 200
		if end > len(values) {
			end = len(values)
		}
		var rows []struct {
			AuthType int
			Identity string
			Type     string
		}
		if err := db.WithContext(ctx).Table("usage_identities").Select("auth_type, identity, type").Where("auth_type IN ? AND identity IN ?", []int{1, 2}, values[offset:end]).Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			key := strconv.Itoa(row.AuthType) + "|" + row.Identity
			if previous, exists := result[key]; exists && previous != row.Type {
				return nil, fmt.Errorf("ambiguous usage identity %s", key)
			}
			result[key] = row.Type
		}
	}
	return result, nil
}

var pricingM2MetadataFields = []string{"provider", "endpoint", "auth_type", "request_id"}

func pricingM2Blank(value string) bool {
	return value == "" || value == "NULL" || strings.TrimSpace(pricingM2Text(value)) == ""
}

func pricingM2QuoteText(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

// pricingM2MetadataPage 按旧 inbox ID 顺序重放本页受影响事件的首次空值补齐。
// event_key 是旧迁移的查找键；只有它查不到事件才退回 payload.request_id。
func pricingM2MetadataPage(ctx context.Context, backupDB, liveDB *gorm.DB, b PricingLegacyBaseline, oldRows []pricingM2EventRow) (map[int64]map[string]string, error) {
	redisPending := pricingM2MigrationPending(b, "20260503_backfill_usage_event_redis_fields")
	identityPending := pricingM2MigrationPending(b, "20260504_backfill_usage_event_identity_fields")
	if !redisPending && !identityPending {
		return nil, nil
	}
	result := make(map[int64]map[string]string, len(oldRows))
	for _, old := range oldRows {
		values := make(map[string]string, len(pricingM2MetadataFields))
		for _, field := range pricingM2MetadataFields {
			if raw, exists := old.Values[field]; exists {
				values[field] = raw
			} else {
				values[field] = "NULL"
			}
		}
		result[old.ID] = values
	}
	if redisPending {
		if err := pricingM2ReplayInboxPage(ctx, liveDB, oldRows, result); err != nil {
			return nil, err
		}
	}
	if identityPending {
		// 一页内相同身份只查一次原备份，且已有完整 metadata 的事件不查身份。
		type identityResult struct {
			found    bool
			provider string
		}
		identityCache := map[string]identityResult{}
		lookup := func(kind int, quoted string) (bool, string, error) {
			key := strconv.Itoa(kind) + "|" + quoted
			if cached, ok := identityCache[key]; ok {
				return cached.found, cached.provider, nil
			}
			found, provider, err := pricingM2OriginalIdentity(ctx, backupDB, b, kind, quoted)
			if err == nil {
				identityCache[key] = identityResult{found: found, provider: provider}
			}
			return found, provider, err
		}
		for _, old := range oldRows {
			values := result[old.ID]
			source := old.Values["source"]
			authIndex := old.Values["auth_index"]
			// 先按 source 查 AI Provider，再按 auth_index 查 Auth File；与 20260504 SQL 顺序一致。
			if pricingM2Blank(values["auth_type"]) || pricingM2Blank(values["provider"]) {
				aiFound, aiProvider, err := lookup(2, source)
				if err != nil {
					return nil, err
				}
				if aiFound {
					if pricingM2Blank(values["auth_type"]) {
						values["auth_type"] = pricingM2QuoteText("apikey")
					}
					if pricingM2Blank(values["provider"]) && strings.TrimSpace(aiProvider) != "" {
						values["provider"] = pricingM2QuoteText(strings.TrimSpace(aiProvider))
					}
				}
			}
			if pricingM2Blank(values["auth_type"]) {
				oauthFound, _, err := lookup(1, authIndex)
				if err != nil {
					return nil, err
				}
				if oauthFound {
					values["auth_type"] = pricingM2QuoteText("oauth")
				}
			}
		}
	}
	return result, nil
}

type pricingM2InboxPayload struct {
	Provider  string `json:"provider"`
	Endpoint  string `json:"endpoint"`
	AuthType  string `json:"auth_type"`
	RequestID string `json:"request_id"`
}

// pricingM2ReplayInboxPage 只扫描本页 event_key 的相关 processed 行，保持旧迁移的 ID 顺序和 fallback 目标选择。
func pricingM2ReplayInboxPage(ctx context.Context, db *gorm.DB, events []pricingM2EventRow, values map[int64]map[string]string) error {
	keys := []string{}
	seen := map[string]bool{}
	for _, event := range events {
		rawKey := event.Values["event_key"]
		if pricingM2Blank(rawKey) {
			continue
		}
		key := pricingM2Text(rawKey)
		if seen[key] {
			return fmt.Errorf("duplicate event_key %q in original page", key)
		}
		seen[key] = true
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		return nil
	}
	if !db.Migrator().HasTable("redis_usage_inboxes") {
		return fmt.Errorf("processed inbox missing for legacy metadata conversion")
	}
	placeholders := make([]string, len(keys))
	args := make([]any, len(keys))
	for i, key := range keys {
		placeholders[i] = "(?)"
		args[i] = key
	}
	requestExpr := "CASE WHEN json_valid(i.raw_message) THEN TRIM(COALESCE(json_extract(i.raw_message, '$.request_id'), '')) ELSE '' END"
	firstKey := "TRIM(COALESCE(i.usage_event_key, ''))"
	query := "WITH keys(key) AS (VALUES " + strings.Join(placeholders, ",") + ") " +
		"SELECT i.id, COALESCE(i.usage_event_key, ''), i.raw_message, " +
		"COALESCE((SELECT MIN(e.id) FROM usage_events e WHERE e.event_key = " + firstKey + "), 0), " +
		"(SELECT COUNT(*) FROM usage_events e WHERE e.event_key = " + firstKey + "), " +
		"COALESCE((SELECT MIN(e.id) FROM usage_events e WHERE e.event_key = " + requestExpr + "), 0), " +
		"(SELECT COUNT(*) FROM usage_events e WHERE e.event_key = " + requestExpr + ") " +
		"FROM redis_usage_inboxes i WHERE i.status = 'processed' AND (" + firstKey + " IN (SELECT key FROM keys) OR " + requestExpr + " IN (SELECT key FROM keys)) ORDER BY i.id"
	rows, err := db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return fmt.Errorf("read processed metadata inbox: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var inboxID, firstID, firstCount, fallbackID, fallbackCount int64
		var eventKey, raw string
		if err := rows.Scan(&inboxID, &eventKey, &raw, &firstID, &firstCount, &fallbackID, &fallbackCount); err != nil {
			return fmt.Errorf("scan processed metadata inbox: %w", err)
		}
		var payload pricingM2InboxPayload
		if json.Unmarshal([]byte(raw), &payload) != nil {
			continue
		}
		payload.Provider = strings.TrimSpace(payload.Provider)
		payload.Endpoint = strings.TrimSpace(payload.Endpoint)
		payload.AuthType = strings.ToLower(strings.TrimSpace(payload.AuthType))
		if payload.AuthType == "api_key" {
			payload.AuthType = "apikey"
		}
		payload.RequestID = strings.TrimSpace(payload.RequestID)
		if payload.Provider == "" && payload.Endpoint == "" && payload.AuthType == "" && payload.RequestID == "" {
			continue
		}
		key := strings.TrimSpace(eventKey)
		targetID := int64(0)
		if key != "" {
			if firstCount > 1 {
				return fmt.Errorf("inbox %d event_key %q matches multiple old events", inboxID, key)
			}
			targetID = firstID
		}
		if targetID == 0 && payload.RequestID != "" && payload.RequestID != key {
			if fallbackCount > 1 {
				return fmt.Errorf("inbox %d request_id %q matches multiple old events", inboxID, payload.RequestID)
			}
			targetID = fallbackID
		}
		current := values[targetID]
		if current == nil {
			continue
		}
		for field, value := range map[string]string{"provider": payload.Provider, "endpoint": payload.Endpoint, "auth_type": payload.AuthType, "request_id": payload.RequestID} {
			if value != "" && pricingM2Blank(current[field]) {
				current[field] = pricingM2QuoteText(value)
			}
		}
	}
	return rows.Err()
}

// pricingM2OriginalIdentity 从 M1 物理表恢复 20260504 运行时会看到的身份键。
func pricingM2OriginalIdentity(ctx context.Context, db *gorm.DB, b PricingLegacyBaseline, authType int, identityQuoted string) (bool, string, error) {
	if pricingM2Blank(identityQuoted) {
		return false, "", nil
	}
	identity := pricingM2Text(identityQuoted)
	if pricingM2MigrationPending(b, "20260504_migrate_usage_identities_metadata") {
		table, key, provider := "auth_files", "auth_index", "provider"
		if authType == 2 {
			table, key, provider = "provider_metadata", "lookup_key", "display_name"
		}
		if hasPricingBaselineTable(b, table) {
			if !hasPricingBaselineColumn(b, table, key) || !hasPricingBaselineColumn(b, table, provider) {
				return false, "", fmt.Errorf("%s lacks old identity metadata columns", table)
			}
			var row struct{ Provider sql.NullString }
			query := db.WithContext(ctx).Table(table).Select(provider+" AS provider").Where(key+" = ?", identity).Order("rowid DESC").Limit(1).Scan(&row)
			if query.Error != nil {
				return false, "", query.Error
			}
			if query.RowsAffected > 0 {
				return true, row.Provider.String, nil
			}
		}
	}
	if !hasPricingBaselineTable(b, "usage_identities") {
		return false, "", nil
	}
	if !hasPricingBaselineColumn(b, "usage_identities", "auth_type") || !hasPricingBaselineColumn(b, "usage_identities", "identity") || !hasPricingBaselineColumn(b, "usage_identities", "provider") {
		return false, "", fmt.Errorf("original usage_identities lacks identity metadata columns")
	}
	var row struct{ Provider sql.NullString }
	query := db.WithContext(ctx).Table("usage_identities").Select("provider").Where("auth_type = ? AND identity = ?", authType, identity).Limit(1).Scan(&row)
	if query.Error != nil {
		return false, "", query.Error
	}
	return query.RowsAffected > 0, row.Provider.String, nil
}

var pricingM2Dimensions = []string{
	"api_group_key", "model", "auth_index", "model_alias",
	"service_tier", "response_service_tier", "reasoning_effort", "endpoint", "executor_type",
}

var pricingM2Counts = []string{
	"request_count", "success_count", "failure_count", "input_tokens", "output_tokens",
	"reasoning_tokens", "cached_tokens", "cache_read_tokens", "cache_creation_tokens", "total_tokens",
}

type pricingM2GroupRow struct {
	Key    []string
	Counts []int64
}

// verifyPricingM2Rollup 在给定实际 schema 与水位下核对每个历史分组的请求和 Token，供 M1 原组及 M2 新组共用。
func verifyPricingM2Rollup(ctx context.Context, db *gorm.DB, schema PricingLegacyBaseline, table string, cursor int64) error {
	if !hasPricingBaselineTable(schema, table) {
		if cursor != 0 {
			return fmt.Errorf("%s missing with nonzero cursor %d", table, cursor)
		}
		return nil
	}
	if !hasPricingBaselineColumn(schema, table, "bucket_start") || !hasPricingBaselineColumn(schema, table, "request_count") {
		return fmt.Errorf("%s lacks grouping or count columns", table)
	}
	if !hasPricingBaselineTable(schema, "usage_events") {
		return fmt.Errorf("usage_events missing for %s verification", table)
	}
	dims := []string{}
	for _, field := range pricingM2Dimensions {
		if hasPricingBaselineColumn(schema, table, field) {
			dims = append(dims, field)
		}
	}
	counts := []string{}
	for _, field := range pricingM2Counts {
		if hasPricingBaselineColumn(schema, table, field) {
			counts = append(counts, field)
		}
	}
	// 原库汇总列若没有对应原始明细列，不能用零值假装迁移前分组完整。
	sources := []string{}
	for _, source := range []string{"usage_events", "usage_events_archive"} {
		if !hasPricingBaselineTable(schema, source) {
			continue
		}
		var covered int64
		if err := db.WithContext(ctx).Table(source).Where("id <= ?", cursor).Count(&covered).Error; err != nil {
			return fmt.Errorf("count %s at cursor %d: %w", source, cursor, err)
		}
		if covered == 0 {
			continue
		}
		sources = append(sources, source)
		for _, field := range dims {
			if !hasPricingBaselineColumn(schema, source, field) {
				return fmt.Errorf("%s.%s cannot prove %s", source, field, table)
			}
		}
		for _, field := range counts {
			if field == "request_count" || field == "success_count" || field == "failure_count" {
				if field != "request_count" && !hasPricingBaselineColumn(schema, source, "failed") {
					return fmt.Errorf("%s.failed cannot prove %s", source, table)
				}
				continue
			}
			if !hasPricingBaselineColumn(schema, source, field) {
				return fmt.Errorf("%s.%s cannot prove %s", source, field, table)
			}
		}
	}
	if len(sources) == 0 {
		var rows int64
		if err := db.WithContext(ctx).Table(table).Count(&rows).Error; err != nil {
			return err
		}
		if rows != 0 {
			return fmt.Errorf("%s has %d rows without cursor-covered events", table, rows)
		}
		return nil
	}
	eventQuery := pricingM2EventGroupsSQL(table, dims, counts, sources)
	statQuery := pricingM2StatGroupsSQL(table, dims, counts)
	args := make([]any, len(sources))
	for i := range args {
		args[i] = cursor
	}
	query := "WITH event_groups AS (" + eventQuery + "), stored_groups AS (" + statQuery + "), " +
		"missing AS (SELECT * FROM event_groups EXCEPT SELECT * FROM stored_groups), " +
		"extra AS (SELECT * FROM stored_groups EXCEPT SELECT * FROM event_groups) " +
		"SELECT 'event' AS side, * FROM missing UNION ALL SELECT 'stored' AS side, * FROM extra LIMIT 1"
	rows, err := db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return fmt.Errorf("compare %s event and stored groups: %w", table, err)
	}
	defer rows.Close()
	if !rows.Next() {
		return rows.Err()
	}
	var side string
	group := pricingM2GroupRow{Key: make([]string, len(dims)+1), Counts: make([]int64, len(counts)+1)}
	dest := []any{&side}
	for i := range group.Key {
		dest = append(dest, &group.Key[i])
	}
	for i := range group.Counts {
		dest = append(dest, &group.Counts[i])
	}
	if err := rows.Scan(dest...); err != nil {
		return fmt.Errorf("scan %s group difference: %w", table, err)
	}
	return fmt.Errorf("%s %s group %v differs in fields %v: %v", table, side, group.Key, counts, group.Counts)
}

func pricingM2BucketExpr(column, table string) string {
	value := "CAST(" + column + " AS TEXT)"
	if strings.Contains(table, "daily") {
		return "substr(" + value + ", 1, 10)"
	}
	// 旧 Overview 在 20260512 时间正规化之后才建表；有 offset 的旧值按绝对整小时分组。
	// 先剥小数秒并保留 Z／原 offset，避免不同 SQLite 版本将 .999999999 舍入到下一小时。
	second := "substr(" + value + ", 1, instr(" + value + ", '.') - 1)"
	withoutFraction := "CASE WHEN instr(" + value + ", '.') = 0 THEN " + value +
		" WHEN substr(" + value + ", -1) = 'Z' THEN " + second + " || 'Z'" +
		" WHEN substr(" + value + ", -6, 1) IN ('+', '-') THEN " + second + " || substr(" + value + ", -6)" +
		" ELSE " + second + " END"
	return "CAST(CAST(strftime('%s', " + withoutFraction + ") AS INTEGER) / 3600 AS TEXT)"
}

func pricingM2DimensionExpr(field string) string {
	value := "TRIM(COALESCE(" + field + ", ''))"
	if field == "api_group_key" || field == "model" {
		return "CASE WHEN " + value + " = '' THEN 'unknown' ELSE " + value + " END"
	}
	return value
}

func pricingM2EventGroupsSQL(table string, dims, counts, sources []string) string {
	selects := []string{pricingM2BucketExpr("timestamp", table) + " AS bucket_key"}
	for _, field := range dims {
		selects = append(selects, pricingM2DimensionExpr(field)+" AS "+field)
	}
	selects = append(selects, "COALESCE(failed, 0) AS failed")
	for _, field := range counts {
		if field == "request_count" || field == "success_count" || field == "failure_count" {
			continue
		}
		selects = append(selects, "COALESCE("+field+", 0) AS "+field)
	}
	source := func(name string) string {
		return "SELECT " + strings.Join(selects, ", ") + " FROM " + name + " WHERE id <= ?"
	}
	parts := make([]string, 0, len(sources))
	for _, name := range sources {
		parts = append(parts, source(name))
	}
	union := strings.Join(parts, " UNION ALL ")
	keys := []string{"bucket_key"}
	keys = append(keys, dims...)
	aggregates := []string{}
	for _, field := range counts {
		switch field {
		case "request_count":
			aggregates = append(aggregates, "COUNT(*)")
		case "success_count":
			aggregates = append(aggregates, "SUM(CASE WHEN failed = 0 THEN 1 ELSE 0 END)")
		case "failure_count":
			aggregates = append(aggregates, "SUM(CASE WHEN failed <> 0 THEN 1 ELSE 0 END)")
		default:
			aggregates = append(aggregates, "SUM("+field+")")
		}
	}
	aggregates = append(aggregates, "1 AS row_count")
	return "SELECT " + strings.Join(append(keys, aggregates...), ", ") + " FROM (" + union + ") GROUP BY " + strings.Join(keys, ", ")
}

func pricingM2StatGroupsSQL(table string, dims, counts []string) string {
	keys := []string{pricingM2BucketExpr("bucket_start", table) + " AS bucket_key"}
	for _, field := range dims {
		keys = append(keys, pricingM2DimensionExpr(field)+" AS "+field)
	}
	aggregates := []string{}
	for _, field := range counts {
		aggregates = append(aggregates, "SUM("+field+")")
	}
	aggregates = append(aggregates, "COUNT(*)")
	positions := []string{}
	for i := range keys {
		positions = append(positions, strconv.Itoa(i+1))
	}
	return "SELECT " + strings.Join(append(keys, aggregates...), ", ") + " FROM " + table + " GROUP BY " + strings.Join(positions, ", ")
}
