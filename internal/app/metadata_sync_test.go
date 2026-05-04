package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type metadataSyncStub struct {
	calls int
	errs  []error
}

func (s *metadataSyncStub) SyncMetadata(context.Context) error {
	s.calls++
	call := s.calls
	if len(s.errs) >= call {
		return s.errs[call-1]
	}
	if len(s.errs) > 0 {
		return s.errs[len(s.errs)-1]
	}
	return nil
}

func TestMetadataSyncRunnerRunsImmediatelyThenAtInterval(t *testing.T) {
	syncer := &metadataSyncStub{}
	runner := NewMetadataSyncRunner(syncer, 15*time.Minute)
	var delays []time.Duration
	runner.sleep = func(_ context.Context, d time.Duration) bool {
		delays = append(delays, d)
		return len(delays) < 3
	}

	if err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if syncer.calls != 2 {
		t.Fatalf("expected two metadata sync calls, got %d", syncer.calls)
	}
	expected := []time.Duration{0, 15 * time.Minute, 15 * time.Minute}
	if len(delays) != len(expected) {
		t.Fatalf("expected delays %+v, got %+v", expected, delays)
	}
	for i, want := range expected {
		if delays[i] != want {
			t.Fatalf("expected delay %d to be %s, got %s", i, want, delays[i])
		}
	}
}

func TestMetadataSyncRunnerLogsFailureAndContinues(t *testing.T) {
	logs := captureAppInfoLogs(t)
	syncer := &metadataSyncStub{errs: []error{errors.New("metadata endpoint failed"), nil}}
	runner := NewMetadataSyncRunner(syncer, time.Minute)
	sleepCalls := 0
	runner.sleep = func(context.Context, time.Duration) bool {
		sleepCalls++
		return sleepCalls < 3
	}

	if err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if syncer.calls != 2 {
		t.Fatalf("expected runner to continue after metadata error, got %d calls", syncer.calls)
	}
	content := logs.String()
	if !strings.Contains(content, "level=error") || !strings.Contains(content, "msg=\"metadata sync failed\"") {
		t.Fatalf("expected metadata sync failure error log, got %q", content)
	}
}

func TestMetadataSyncRunnerValidatesConfig(t *testing.T) {
	if err := NewMetadataSyncRunner(nil, time.Minute).Run(context.Background()); err == nil {
		t.Fatal("expected nil syncer validation error")
	}
	if err := NewMetadataSyncRunner(&metadataSyncStub{}, 0).Run(context.Background()); err == nil {
		t.Fatal("expected non-positive interval validation error")
	}
}
