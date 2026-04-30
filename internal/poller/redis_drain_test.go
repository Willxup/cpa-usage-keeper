package poller

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"cpa-usage-keeper/internal/service"
	"github.com/sirupsen/logrus"
)

type redisDrainSyncStub struct {
	mu             sync.Mutex
	pullResults    []*service.RedisInboxPullResult
	pullErrs       []error
	processResults []*service.RedisBatchSyncResult
	processErrs    []error
	metadataFlags  []bool
	pullStarted    chan struct{}
	releasePull    chan struct{}
	pullCalls      int
	processCalls   int
	metadataCalls  int
	metadataErr    error
}

func (s *redisDrainSyncStub) PullRedisUsageInbox(context.Context) (*service.RedisInboxPullResult, error) {
	s.mu.Lock()
	s.pullCalls++
	call := s.pullCalls
	result := &service.RedisInboxPullResult{Status: "completed", InsertedRows: 1}
	if len(s.pullResults) >= call {
		result = s.pullResults[call-1]
	} else if len(s.pullResults) > 0 {
		result = s.pullResults[len(s.pullResults)-1]
	}
	var err error
	if len(s.pullErrs) >= call {
		err = s.pullErrs[call-1]
	} else if len(s.pullErrs) > 0 {
		err = s.pullErrs[len(s.pullErrs)-1]
	}
	pullStarted := s.pullStarted
	releasePull := s.releasePull
	s.mu.Unlock()
	if pullStarted != nil {
		close(pullStarted)
	}
	if releasePull != nil {
		<-releasePull
	}
	return result, err
}

func (s *redisDrainSyncStub) ProcessRedisUsageInbox(ctx context.Context, syncMetadata bool) (*service.RedisBatchSyncResult, error) {
	s.mu.Lock()
	s.processCalls++
	call := s.processCalls
	s.metadataFlags = append(s.metadataFlags, syncMetadata)
	result := &service.RedisBatchSyncResult{Status: "completed", InsertedEvents: 1}
	if len(s.processResults) >= call {
		result = s.processResults[call-1]
	} else if len(s.processResults) > 0 {
		result = s.processResults[len(s.processResults)-1]
	}
	var err error
	if len(s.processErrs) >= call {
		err = s.processErrs[call-1]
	} else if len(s.processErrs) > 0 {
		err = s.processErrs[len(s.processErrs)-1]
	}
	s.mu.Unlock()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	return result, err
}

func (s *redisDrainSyncStub) SyncMetadata(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.metadataCalls++
	return s.metadataErr
}

func (s *redisDrainSyncStub) counts() (int, int, int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pullCalls, s.processCalls, 0, s.metadataCalls
}

func (s *redisDrainSyncStub) flags() []bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]bool(nil), s.metadataFlags...)
}

func captureRedisDrainLogrusOutput(t *testing.T) *bytes.Buffer {
	t.Helper()
	previousOutput := logrus.StandardLogger().Out
	previousFormatter := logrus.StandardLogger().Formatter
	previousLevel := logrus.GetLevel()
	var logs bytes.Buffer
	logrus.SetOutput(&logs)
	logrus.SetFormatter(&logrus.TextFormatter{DisableTimestamp: true})
	logrus.SetLevel(logrus.InfoLevel)
	t.Cleanup(func() {
		logrus.SetOutput(previousOutput)
		logrus.SetFormatter(previousFormatter)
		logrus.SetLevel(previousLevel)
	})
	return &logs
}

func TestRedisDrainLoopsLogTaskStarts(t *testing.T) {
	logs := captureRedisDrainLogrusOutput(t)
	syncer := &redisDrainSyncStub{pullResults: []*service.RedisInboxPullResult{{Empty: true, Status: "empty"}}}
	drain := NewRedisDrain(syncer, RedisDrainConfig{IdleInterval: time.Hour, ErrorBackoff: time.Hour, MetadataInterval: time.Hour})

	pullCtx, cancelPull := context.WithCancel(context.Background())
	drain.sleep = func(context.Context, time.Duration) bool {
		cancelPull()
		return false
	}
	drain.runPullLoop(pullCtx)

	processCtx, cancelProcess := context.WithCancel(context.Background())
	drain.sleep = func(context.Context, time.Duration) bool {
		cancelProcess()
		return false
	}
	drain.runProcessLoop(processCtx)

	content := logs.String()
	for _, expected := range []string{"msg=\"redis inbox pull task started\"", "msg=\"redis inbox process task started\""} {
		if !strings.Contains(content, expected) {
			t.Fatalf("expected redis drain start log %q, got %q", expected, content)
		}
	}
}

func TestRedisDrainPullLoopDoesNotProcessInbox(t *testing.T) {
	syncer := &redisDrainSyncStub{pullResults: []*service.RedisInboxPullResult{{Empty: true, Status: "empty"}}}
	drain := NewRedisDrain(syncer, RedisDrainConfig{IdleInterval: time.Hour, ErrorBackoff: time.Hour, MetadataInterval: time.Hour})
	ctx, cancel := context.WithCancel(context.Background())
	drain.sleep = func(context.Context, time.Duration) bool {
		cancel()
		return false
	}

	drain.runPullLoop(ctx)

	pulls, processes, _, _ := syncer.counts()
	if pulls != 1 || processes != 0 {
		t.Fatalf("expected pull loop to pull once and not process inbox, got pulls=%d processes=%d", pulls, processes)
	}
}

func TestRedisDrainProcessLoopUsesFixedInterval(t *testing.T) {
	syncer := &redisDrainSyncStub{}
	drain := NewRedisDrain(syncer, RedisDrainConfig{IdleInterval: time.Hour, ErrorBackoff: time.Hour, MetadataInterval: time.Hour})
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	drain.sleep = func(_ context.Context, d time.Duration) bool {
		calls++
		if calls == 1 {
			if d != redisInboxProcessInterval {
				t.Fatalf("expected process interval %s, got %s", redisInboxProcessInterval, d)
			}
			return true
		}
		cancel()
		return false
	}

	drain.runProcessLoop(ctx)

	_, processes, _, _ := syncer.counts()
	if processes != 1 {
		t.Fatalf("expected process loop to process once, got %d", processes)
	}
	if calls == 0 {
		t.Fatal("expected process loop to sleep before processing")
	}
}

func TestRedisDrainSyncNowPullsThenProcesses(t *testing.T) {
	syncer := &redisDrainSyncStub{}
	drain := NewRedisDrain(syncer, RedisDrainConfig{IdleInterval: time.Hour, ErrorBackoff: time.Hour, MetadataInterval: time.Hour})

	if err := drain.SyncNow(context.Background()); err != nil {
		t.Fatalf("SyncNow returned error: %v", err)
	}

	pulls, processes, _, _ := syncer.counts()
	if pulls != 1 || processes != 1 {
		t.Fatalf("expected SyncNow to pull and process once, got pulls=%d processes=%d", pulls, processes)
	}
	flags := syncer.flags()
	if len(flags) != 1 || !flags[0] {
		t.Fatalf("expected SyncNow processing to sync metadata, got %v", flags)
	}
}

func TestRedisDrainPullAndProcessCanRunIndependently(t *testing.T) {
	syncer := &redisDrainSyncStub{pullStarted: make(chan struct{}), releasePull: make(chan struct{})}
	drain := NewRedisDrain(syncer, RedisDrainConfig{IdleInterval: time.Hour, ErrorBackoff: time.Hour, MetadataInterval: time.Hour})
	ctx := context.Background()
	pullDone := make(chan error, 1)
	go func() {
		_, err := drain.runRedisPull(ctx)
		pullDone <- err
	}()
	<-syncer.pullStarted

	if _, err := drain.runRedisProcess(ctx, false); err != nil {
		close(syncer.releasePull)
		t.Fatalf("expected process to run while pull is active, got %v", err)
	}
	close(syncer.releasePull)
	if err := <-pullDone; err != nil {
		t.Fatalf("pull returned error: %v", err)
	}

	pulls, processes, _, _ := syncer.counts()
	if pulls != 1 || processes != 1 {
		t.Fatalf("expected pull and process to each run once, got pulls=%d processes=%d", pulls, processes)
	}
}

func TestRedisDrainBacksOffAfterPullError(t *testing.T) {
	syncer := &redisDrainSyncStub{pullErrs: []error{errors.New("dial failed")}}
	drain := NewRedisDrain(syncer, RedisDrainConfig{IdleInterval: time.Hour, ErrorBackoff: 25 * time.Millisecond, MetadataInterval: time.Hour})
	ctx, cancel := context.WithCancel(context.Background())
	var slept time.Duration
	drain.sleep = func(_ context.Context, d time.Duration) bool {
		slept = d
		cancel()
		return false
	}

	drain.runPullLoop(ctx)

	if slept != 25*time.Millisecond {
		t.Fatalf("expected error backoff sleep, got %s", slept)
	}
	status := drain.Status()
	if status.LastError != "dial failed" {
		t.Fatalf("expected recorded pull error, got %+v", status)
	}
}
