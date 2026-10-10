package test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/releasecheck"
	"cpa-usage-keeper/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func database(t *testing.T, path string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&entities.AppSetting{}); err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func reply(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func TestSuccessPersistsAcrossRestartUpgradeAndRollback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	db := database(t, path)
	calls := 0
	client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "github.com" || r.URL.Scheme != "https" || r.URL.RawQuery != "" || len(r.Header) != 0 {
			t.Fatalf("unexpected request: %s headers=%v", r.URL, r.Header)
		}
		v := strings.Split(r.URL.Path, "/")[5]
		if r.URL.Path != "/Willxup/cpa-usage-keeper/releases/download/"+v+"/version.txt" {
			t.Fatal(r.URL)
		}
		return reply(200, v+"\n"), nil
	})}
	now := time.Now()
	for _, version := range []string{"v2.0.0", "v2.0.0", "v2.0.1", "v2.0.0"} {
		wait, err := releasecheck.New(db, version, client).Check(context.Background(), now)
		if err != nil || wait != 0 {
			t.Fatalf("%s: wait=%s err=%v", version, wait, err)
		}
	}
	db2 := database(t, path)
	if _, err := releasecheck.New(db2, "v2.0.0", client).Check(context.Background(), now.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("requests=%d, want 2", calls)
	}
}

func TestFailuresPersistIntervalAndFiveAttemptLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	db := database(t, path)
	calls := 0
	client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) { calls++; return reply(404, "missing"), nil })}
	now := time.Date(2026, 10, 9, 1, 0, 0, 0, time.Local)
	for i := range 5 {
		runner := releasecheck.New(db, "v2.0.0", client)
		wait, err := runner.Check(context.Background(), now.Add(time.Duration(i)*6*time.Hour))
		if err == nil {
			t.Fatal("missing attachment must not count as success")
		}
		if i < 4 && wait != 6*time.Hour {
			t.Fatalf("wait=%s", wait)
		}
		if i == 4 && wait != 0 {
			t.Fatal("fifth failure must stop")
		}
		before := calls
		_, err = releasecheck.New(database(t, path), "v2.0.0", client).Check(context.Background(), now.Add(time.Duration(i)*6*time.Hour+time.Hour))
		if err != nil || calls != before {
			t.Fatalf("restart bypassed interval: %v", err)
		}
	}
	_, err := releasecheck.New(db, "v2.0.0", client).Check(context.Background(), now.Add(30*24*time.Hour))
	if err != nil || calls != 5 {
		t.Fatalf("exhausted version retried: calls=%d err=%v", calls, err)
	}
	_, _ = releasecheck.New(db, "v2.0.1", client).Check(context.Background(), now)
	if calls != 6 {
		t.Fatal("new version must have its own budget")
	}
}

func TestSkippedBuildsDoNotTouchDatabaseOrNetwork(t *testing.T) {
	client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) { t.Fatal("unexpected download"); return nil, nil })}
	for _, v := range []string{"dev", "dev-1234567", "next", "v1.15.10", "v2.0.0-beta.1", "v2.0.0+build", "v2.01.0", "v2.0.0/other"} {
		wait, err := releasecheck.New(nil, v, client).Check(context.Background(), time.Now())
		if wait != 0 || err != nil {
			t.Fatalf("%s: %s %v", v, wait, err)
		}
	}
}

func TestInvalidResponseDoesNotCompleteAndCanRecover(t *testing.T) {
	for _, body := range []string{"v2.0.1", "", "<html>error</html>", strings.Repeat(" ", 1025) + "v2.0.0"} {
		t.Run(body[:min(len(body), 12)], func(t *testing.T) {
			db := database(t, filepath.Join(t.TempDir(), "state.db"))
			calls := 0
			client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return reply(200, body), nil
				}
				return reply(200, "v2.0.0\n"), nil
			})}
			r := releasecheck.New(db, "v2.0.0", client)
			now := time.Now()
			if _, err := r.Check(context.Background(), now); err == nil {
				t.Fatal("invalid body accepted")
			}
			if wait, err := r.Check(context.Background(), now.Add(6*time.Hour)); wait != 0 || err != nil {
				t.Fatalf("recovery: %s %v", wait, err)
			}
			_, _ = r.Check(context.Background(), now.Add(12*time.Hour))
			if calls != 2 {
				t.Fatal("successful retry was not persisted")
			}
		})
	}
}

func TestNetworkFailureAndCancellation(t *testing.T) {
	db := database(t, filepath.Join(t.TempDir(), "state.db"))
	client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) > 10*time.Second {
			t.Fatal("missing request deadline")
		}
		return nil, context.DeadlineExceeded
	})}
	r := releasecheck.New(db, "v2.0.0", client)
	if wait, err := r.Check(context.Background(), time.Now()); wait != 6*time.Hour || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("%s %v", wait, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runner did not stop")
	}
}

func TestConcurrentChecksOnlyClaimOneAttempt(t *testing.T) {
	db := database(t, filepath.Join(t.TempDir(), "state.db"))
	started, finish := make(chan struct{}), make(chan struct{})
	client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		close(started)
		<-finish
		return reply(200, "v2.0.0"), nil
	})}
	done := make(chan error, 1)
	now := time.Now()
	go func() { _, err := releasecheck.New(db, "v2.0.0", client).Check(context.Background(), now); done <- err }()
	<-started
	wait, err := releasecheck.New(db, "v2.0.0", client).Check(context.Background(), now)
	close(finish)
	if err != nil || wait != 6*time.Hour {
		t.Fatalf("second claim: %s %v", wait, err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestCompletionWriteRetriesWithoutAnotherDownload(t *testing.T) {
	for _, failures := range []int{1, 3, 4} {
		t.Run(strings.Repeat("failure", failures), func(t *testing.T) {
			t.Parallel()
			db := database(t, filepath.Join(t.TempDir(), "state.db"))
			writes, downloads := 0, 0
			var writeTimes []time.Time
			injected := errors.New("temporary completion write failure")
			if err := db.Callback().Create().Before("gorm:create").Register("test:completion", func(tx *gorm.DB) {
				writes++
				if writes == 1 {
					return
				} // Initial attempt reservation succeeds.
				writeTimes = append(writeTimes, time.Now())
				if writes <= failures+1 {
					tx.AddError(injected)
				}
			}); err != nil {
				t.Fatal(err)
			}
			client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
				downloads++
				return reply(200, "v2.0.0"), nil
			})}
			now := time.Now()
			wait, err := releasecheck.New(db, "v2.0.0", client).Check(context.Background(), now)
			if wait != 0 || (failures < 4 && err != nil) || (failures == 4 && !errors.Is(err, injected)) {
				t.Fatalf("wait=%v err=%v", wait, err)
			}
			if downloads != 1 || len(writeTimes) != min(failures+1, 4) {
				t.Fatalf("downloads=%d completion writes=%d", downloads, len(writeTimes))
			}
			for i, delay := range []time.Duration{time.Second, 3 * time.Second, 10 * time.Second} {
				if i+1 < len(writeTimes) && writeTimes[i+1].Sub(writeTimes[i]) < delay {
					t.Fatalf("retry %d ran before %v", i+1, delay)
				}
			}
			setting, found, err := repository.GetAppSetting(context.Background(), db, "release.version_file.v2.0.0")
			if err != nil || !found || setting.Value == nil {
				t.Fatalf("saved state missing: %v", err)
			}
			var state struct {
				Attempts int  `json:"attempts"`
				Complete bool `json:"complete"`
			}
			if err := json.Unmarshal([]byte(*setting.Value), &state); err != nil {
				t.Fatal(err)
			}
			if state.Attempts != 1 || state.Complete != (failures < 4) {
				t.Fatalf("state=%+v", state)
			}
			if failures < 4 {
				_, err = releasecheck.New(db, "v2.0.0", client).Check(context.Background(), now.Add(24*time.Hour))
				if err != nil || downloads != 1 {
					t.Fatalf("completed version downloaded again: %v", err)
				}
			}
		})
	}
}

func TestCompletionWriteRetryStopsOnCancellation(t *testing.T) {
	db := database(t, filepath.Join(t.TempDir(), "state.db"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	writes, downloads := 0, 0
	if err := db.Callback().Create().Before("gorm:create").Register("test:cancel_completion", func(tx *gorm.DB) {
		writes++
		if writes == 2 {
			tx.AddError(errors.New("temporary write failure"))
			cancel()
		}
	}); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		downloads++
		return reply(200, "v2.0.0"), nil
	})}
	started := time.Now()
	wait, err := releasecheck.New(db, "v2.0.0", client).Check(ctx, started)
	if wait != 0 || !errors.Is(err, context.Canceled) || writes != 2 || downloads != 1 {
		t.Fatalf("wait=%v err=%v writes=%d downloads=%d", wait, err, writes, downloads)
	}
	if time.Since(started) >= time.Second {
		t.Fatal("cancellation waited for retry timer")
	}
}
