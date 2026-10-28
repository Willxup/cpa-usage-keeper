package test

import (
	"sync"
	"testing"
	"time"

	"cpa-usage-keeper/internal/auth"
)

func TestLoginAttemptLimiterRecoversAfterWindow(t *testing.T) {
	now := time.Date(2026, time.July, 31, 12, 0, 0, 0, time.UTC)
	limiter := auth.NewLoginAttemptLimiter(auth.LoginAttemptLimiterOptions{
		Window:         time.Minute,
		PerSourceLimit: 1,
		GlobalLimit:    10,
		MaxSources:     10,
		Now:            func() time.Time { return now },
	})

	if allowed, _ := limiter.Allow("source-a"); !allowed {
		t.Fatal("expected first attempt to be allowed")
	}
	if allowed, retryAfter := limiter.Allow("source-a"); allowed || retryAfter != time.Minute {
		t.Fatalf("expected source limit with one minute retry, got allowed=%v retry=%s", allowed, retryAfter)
	}

	now = now.Add(time.Minute)
	if allowed, _ := limiter.Allow("source-a"); !allowed {
		t.Fatal("expected attempt to be allowed after the window resets")
	}
}

func TestPendingLoginAttemptsCountOnlyGlobalRequestsUntilFailure(t *testing.T) {
	now := time.Date(2026, time.October, 21, 0, 0, 0, 0, time.UTC)
	limiter := auth.NewLoginAttemptLimiter(auth.LoginAttemptLimiterOptions{Now: func() time.Time { return now }})
	for range 60 {
		if allowed, _ := limiter.AllowPending("oidc:source"); !allowed {
			t.Fatal("unfinished login consumed the source failure allowance")
		}
	}
	if allowed, wait := limiter.AllowPending("oidc:other"); allowed || wait != time.Minute {
		t.Fatalf("global request limit = %v, %s", allowed, wait)
	}
	now = now.Add(time.Minute)
	for range 5 {
		if allowed, _ := limiter.AllowPending("oidc:source"); !allowed {
			t.Fatal("source limited before five recorded failures")
		}
		limiter.RecordFailure("oidc:source")
	}
	if allowed, _ := limiter.AllowPending("oidc:source"); allowed {
		t.Fatal("five confirmed failures did not limit the source")
	}
	if allowed, _ := limiter.Allow("source"); !allowed {
		t.Fatal("OIDC failures blocked password login")
	}
	now = now.Add(time.Minute)
	if allowed, _ := limiter.AllowPending("oidc:source"); !allowed {
		t.Fatal("OIDC failure window did not expire")
	}
}

func TestPendingLoginFailureRecordingIsConcurrentAndDoesNotClearOtherFailures(t *testing.T) {
	now := time.Date(2026, time.October, 21, 0, 0, 0, 0, time.UTC)
	limiter := auth.NewLoginAttemptLimiter(auth.LoginAttemptLimiterOptions{PerSourceLimit: 20, GlobalLimit: 100, Now: func() time.Time { return now }})
	var workers sync.WaitGroup
	for range 19 {
		workers.Go(func() { limiter.RecordFailure("oidc:source") })
	}
	workers.Wait()
	for range 6 {
		if allowed, _ := limiter.AllowPending("oidc:source"); !allowed {
			t.Fatal("pending/canceled requests increased failure count")
		}
	}
	limiter.RecordFailure("oidc:source")
	if allowed, _ := limiter.AllowPending("oidc:source"); allowed {
		t.Fatal("concurrent failures were lost or cleared by pending requests")
	}
	limiter.Reset("oidc:source")
	if allowed, _ := limiter.AllowPending("oidc:source"); !allowed {
		t.Fatal("successful authentication could not reset source failures")
	}
}

func TestLoginAttemptLimiterAppliesGlobalLimitAcrossSources(t *testing.T) {
	now := time.Date(2026, time.July, 31, 12, 0, 0, 0, time.UTC)
	limiter := auth.NewLoginAttemptLimiter(auth.LoginAttemptLimiterOptions{
		Window:         time.Minute,
		PerSourceLimit: 5,
		GlobalLimit:    2,
		MaxSources:     10,
		Now:            func() time.Time { return now },
	})

	for _, source := range []string{"source-a", "source-b"} {
		if allowed, _ := limiter.Allow(source); !allowed {
			t.Fatalf("expected %s to be allowed", source)
		}
	}
	if allowed, retryAfter := limiter.Allow("source-c"); allowed || retryAfter != time.Minute {
		t.Fatalf("expected global limit with one minute retry, got allowed=%v retry=%s", allowed, retryAfter)
	}
}

func TestLoginAttemptLimiterBoundsTrackedSources(t *testing.T) {
	now := time.Date(2026, time.July, 31, 12, 0, 0, 0, time.UTC)
	limiter := auth.NewLoginAttemptLimiter(auth.LoginAttemptLimiterOptions{
		Window:         time.Minute,
		PerSourceLimit: 1,
		GlobalLimit:    100,
		MaxSources:     2,
		Now:            func() time.Time { return now },
	})

	if allowed, _ := limiter.Allow("oldest"); !allowed {
		t.Fatal("expected oldest source to be allowed")
	}
	now = now.Add(time.Second)
	if allowed, _ := limiter.Allow("newer"); !allowed {
		t.Fatal("expected newer source to be allowed")
	}
	now = now.Add(time.Second)
	if allowed, _ := limiter.Allow("newest"); !allowed {
		t.Fatal("expected newest source to be allowed")
	}
	if allowed, _ := limiter.Allow("oldest"); !allowed {
		t.Fatal("expected the evicted oldest source to receive a fresh window")
	}
}

func TestLoginAttemptLimiterResetClearsOnlyTheSource(t *testing.T) {
	now := time.Date(2026, time.July, 31, 12, 0, 0, 0, time.UTC)
	limiter := auth.NewLoginAttemptLimiter(auth.LoginAttemptLimiterOptions{
		Window:         time.Minute,
		PerSourceLimit: 1,
		GlobalLimit:    10,
		MaxSources:     10,
		Now:            func() time.Time { return now },
	})

	_, _ = limiter.Allow("source-a")
	_, _ = limiter.Allow("source-b")
	limiter.Reset("source-a")

	if allowed, _ := limiter.Allow("source-a"); !allowed {
		t.Fatal("expected reset source to be allowed")
	}
	if allowed, _ := limiter.Allow("source-b"); allowed {
		t.Fatal("expected another source to remain limited")
	}
}
