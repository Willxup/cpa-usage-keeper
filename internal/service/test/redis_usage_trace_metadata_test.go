package test

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	repositorydto "cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/repository/migration"
	"cpa-usage-keeper/internal/service"
)

type usageTraceMetadata struct {
	ExecutionID  *string
	TraceID      *string
	NodeKind     *string
	IsFork       *bool
	IsCompaction *bool
}

func traceMetadataPointer[T any](value T) *T { return &value }

func usageTraceMetadataCases() []struct {
	name string
	json string
	want usageTraceMetadata
} {
	return []struct {
		name string
		json string
		want usageTraceMetadata
	}{
		{"explicit true", `,"execution_id":"execution-1","trace_id":"000000ab","node_kind":"future-kind","is_fork":true,"is_compaction":true`, usageTraceMetadata{traceMetadataPointer("execution-1"), traceMetadataPointer("000000ab"), traceMetadataPointer("future-kind"), traceMetadataPointer(true), traceMetadataPointer(true)}},
		{"explicit false", `,"execution_id":"execution-2","trace_id":"custom-trace","node_kind":"fork","is_fork":false,"is_compaction":false`, usageTraceMetadata{traceMetadataPointer("execution-2"), traceMetadataPointer("custom-trace"), traceMetadataPointer("fork"), traceMetadataPointer(false), traceMetadataPointer(false)}},
		{"omitted", ``, usageTraceMetadata{}},
		{"explicit null", `,"execution_id":null,"trace_id":null,"node_kind":null,"is_fork":null,"is_compaction":null`, usageTraceMetadata{}},
		{"raw values", `,"execution_id":" execution raw ","trace_id":"","node_kind":" custom node ","is_fork":true,"is_compaction":false`, usageTraceMetadata{traceMetadataPointer(" execution raw "), traceMetadataPointer(""), traceMetadataPointer(" custom node "), traceMetadataPointer(true), traceMetadataPointer(false)}},
	}
}

func traceMetadataFromEvent(event entities.UsageEvent) usageTraceMetadata {
	return usageTraceMetadata{event.ExecutionID, event.TraceID, event.NodeKind, event.IsFork, event.IsCompaction}
}

func TestDecodeRedisUsageMessagePreservesTraceMetadata(t *testing.T) {
	for _, testCase := range usageTraceMetadataCases() {
		t.Run(testCase.name, func(t *testing.T) {
			message := `{"request_id":"shared-request","tokens":{"input_tokens":10,"output_tokens":20,"total_tokens":30}` + testCase.json + `}`
			event, raw, err := service.DecodeRedisUsageMessage(message, time.Now())
			if err != nil {
				t.Fatalf("decode usage message: %v", err)
			}
			if got := traceMetadataFromEvent(event); !reflect.DeepEqual(got, testCase.want) {
				t.Fatalf("metadata=%+v, want %+v", got, testCase.want)
			}
			if event.RequestID != "shared-request" || event.EventKey != "shared-request" || event.TotalTokens != 30 || string(raw) != message {
				t.Fatalf("trace metadata changed existing request/raw/token semantics: %+v", event)
			}
		})
	}
}

func TestRedisUsageTraceMetadataSurvivesInboxSyncArchiveAndReplay(t *testing.T) {
	db := openUsageServiceTestDatabase(t)
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.Local)
	testCases := usageTraceMetadataCases()
	inputs := make([]repositorydto.RedisInboxInsert, 0, len(testCases))
	for index, testCase := range testCases {
		timestamp := now.AddDate(0, 0, -91)
		if index == len(testCases)-1 {
			timestamp = now.Add(-time.Hour)
		}
		message := fmt.Sprintf(`{"request_id":"shared-request","timestamp":%q,"provider":"codex","executor_type":"CodexExecutor","model":"trace-model","auth_type":"apikey","tokens":{"input_tokens":10,"output_tokens":20,"total_tokens":30}%s}`, timestamp.Format(time.RFC3339), testCase.json)
		inputs = append(inputs, repositorydto.RedisInboxInsert{Source: "trace-test", RawMessage: message, PoppedAt: now})
	}
	rows, err := repository.InsertRedisUsageInboxMessages(db, inputs)
	if err != nil {
		t.Fatalf("insert inbox messages: %v", err)
	}
	result, err := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{}).ProcessRedisUsageInbox(context.Background())
	if err != nil {
		t.Fatalf("process inbox messages: %v", err)
	}
	if result.InsertedEvents != len(testCases) || result.DedupedEvents != 0 || result.ProcessedRows != len(testCases) {
		t.Fatalf("all executions of the same request must remain independent events: %+v", result)
	}
	var original []entities.UsageEvent
	if err := db.Order("id ASC").Find(&original).Error; err != nil || len(original) != len(testCases) {
		t.Fatalf("load stored usage events: count=%d, error=%v", len(original), err)
	}
	for index, event := range original {
		if got := traceMetadataFromEvent(event); !reflect.DeepEqual(got, testCases[index].want) {
			t.Fatalf("stored %s metadata=%+v, want %+v", testCases[index].name, got, testCases[index].want)
		}
		if event.EventKey != "shared-request" || event.RequestID != "shared-request" || event.TotalTokens != 30 {
			t.Fatalf("metadata changed request or token semantics: %+v", event)
		}
		var inbox entities.RedisUsageInbox
		if err := db.First(&inbox, rows[index].ID).Error; err != nil {
			t.Fatalf("load processed inbox: %v", err)
		}
		if inbox.RawMessage != inputs[index].RawMessage || inbox.Status != repository.RedisUsageInboxStatusProcessed {
			t.Fatalf("inbox raw data/status changed: %+v", inbox)
		}
	}
	archived, err := repository.ArchiveExpiredUsageEvents(context.Background(), db, now)
	if err != nil || archived.Archived != int64(len(testCases)-1) {
		t.Fatalf("archive old executions: result=%+v, error=%v", archived, err)
	}
	var archiveRows []entities.UsageEvent
	if err := db.Table("usage_events_archive").Order("id ASC").Find(&archiveRows).Error; err != nil {
		t.Fatalf("load archived executions: %v", err)
	}
	if !reflect.DeepEqual(archiveRows, original[:len(testCases)-1]) {
		t.Fatalf("archiving changed original event values: archive=%+v, original=%+v", archiveRows, original)
	}
	targetID, err := migration.LoadUsageAggregationReplayTargetEventID(db)
	if err != nil {
		t.Fatalf("load replay target: %v", err)
	}
	var replayed []entities.UsageEvent
	for afterID := int64(0); afterID < targetID; {
		page, err := migration.LoadUsageAggregationReplayEventPage(db, afterID, targetID, 2)
		if err != nil || len(page) == 0 {
			t.Fatalf("load metadata replay page: count=%d, error=%v", len(page), err)
		}
		replayed = append(replayed, page...)
		afterID = page[len(page)-1].ID
	}
	if !reflect.DeepEqual(replayed, original) {
		t.Fatalf("hot/archive replay changed original event values: replay=%+v, original=%+v", replayed, original)
	}
}
