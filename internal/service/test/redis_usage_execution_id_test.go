package test

import (
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/service"
)

func TestDecodeRedisUsageMessageExecutionIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, fields, requestID, eventKey string
	}{
		{"both IDs", `"request_id":" request-1 ","execution_id":"execution-1"`, "request-1", "request-1"},
		{"request absent", `"execution_id":"execution-1"`, "", "execution-1"},
		{"request null", `"request_id":null,"execution_id":"execution-1"`, "", "execution-1"},
		{"request empty", `"request_id":"","execution_id":"execution-1"`, "", "execution-1"},
		{"request whitespace", `"request_id":" \t ","execution_id":"execution-1"`, "", "execution-1"},
		{"execution raw", `"execution_id":" execution raw "`, "", " execution raw "},
		{"execution whitespace", `"execution_id":" \t "`, "", " \t "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			message := `{` + tc.fields + `,"timestamp":"2026-10-18T01:02:03+08:00","executor_type":"CodexExecutor","trace_id":"trace-1","tokens":{"input_tokens":10,"output_tokens":20,"total_tokens":30}}`
			event, raw, err := service.DecodeRedisUsageMessage(message, time.Now())
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if event.RequestID != tc.requestID || event.EventKey != tc.eventKey {
				t.Fatalf("request=%q key=%q; want request=%q key=%q", event.RequestID, event.EventKey, tc.requestID, tc.eventKey)
			}
			if event.ExecutionID == nil || (tc.requestID == "" && *event.ExecutionID != tc.eventKey) || event.TraceID == nil || *event.TraceID != "trace-1" || event.TotalTokens != 30 || string(raw) != message || event.Timestamp.Unix() != time.Date(2026, 10, 18, 1, 2, 3, 0, time.FixedZone("CPA", 8*60*60)).Unix() {
				t.Fatalf("execution/raw/time/token metadata changed: %+v", event)
			}
		})
	}
}

func TestDecodeRedisUsageMessageRequiresExecutionID(t *testing.T) {
	for _, fields := range []string{`"request_id":"request-1"`, `"request_id":"request-1","execution_id":null`, `"request_id":"request-1","execution_id":""`, `"execution_id":""`} {
		message := `{` + fields + `}`
		_, raw, err := service.DecodeRedisUsageMessage(message, time.Now())
		if err == nil || err.Error() != "decode redis usage message: execution_id is required" || string(raw) != message {
			t.Fatalf("decode %s: raw=%s error=%v", message, raw, err)
		}
	}
	for _, message := range []string{`{"execution_id":123}`, `{"execution_id":{}}`, `{"execution_id":"execution-1","request_id":123}`} {
		if _, _, err := service.DecodeRedisUsageMessage(message, time.Now()); err == nil || !strings.Contains(err.Error(), "cannot unmarshal") {
			t.Fatalf("wrong field type accepted: %s, error=%v", message, err)
		}
	}
}
