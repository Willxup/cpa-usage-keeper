package test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/service/dto"
)

func TestRecalculationTaskKeepsUnknownTotalAndOffset(t *testing.T) {
	location := time.FixedZone("CPA", 8*60*60)
	started := time.Date(2026, 9, 23, 9, 0, 0, 0, location)
	task := dto.RecalculationTask{
		TaskID: "task-1", Status: dto.RecalculationRunning, Stage: dto.RecalculationEvents,
		StartAt: started, EndAt: started.Add(time.Hour), ConfigRevision: 3,
		ProcessedCount: 12, TotalCount: nil, UpdatedAt: started, Error: nil,
	}
	encoded, err := json.Marshal(task)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{`"total_count":null`, `"error":null`, `"start_at":"2026-09-23T09:00:00+08:00"`} {
		if !strings.Contains(string(encoded), fragment) {
			t.Fatalf("task contract lost %s: %s", fragment, encoded)
		}
	}
	count := int64(0)
	task.TotalCount = &count
	encoded, err = json.Marshal(task)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"total_count":0`) {
		t.Fatalf("known empty task count must be zero: %s", encoded)
	}
}

func TestPricingFieldErrorCarriesBothConflictingBranches(t *testing.T) {
	response := dto.PricingErrorResponse{
		Code: "branch_conflict", Message: "分支条件重叠",
		Fields: []dto.PricingFieldError{{Path: "branches[1].context", Code: "conflict", BranchIDs: []string{"a", "b"}}},
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"branch_ids":["a","b"]`) {
		t.Fatalf("conflicting branch IDs missing: %s", encoded)
	}
}
