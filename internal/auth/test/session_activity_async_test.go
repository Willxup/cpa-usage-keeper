package test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"cpa-usage-keeper/internal/auth"
)

func TestTouchDoesNotWaitForActivityPersistence(t *testing.T) {
	release := make(chan struct{})
	store := newActivitySessionStore(release, nil)
	manager := auth.NewPersistentSessionManager(time.Hour, store)
	token, _, err := manager.CreateWithSourceAndMetadata(auth.SessionSourceStandard, auth.SessionClientMetadata{IP: "203.0.113.9"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	var releaseOnce sync.Once
	releaseWrite := func() {
		releaseOnce.Do(func() { close(release) })
	}
	t.Cleanup(releaseWrite)

	touchResult := make(chan bool, 1)
	go func() {
		touchResult <- manager.Touch(token, "198.51.100.42")
	}()

	waitForActivitySignal(t, store.started, "activity persistence to start")
	select {
	case touched := <-touchResult:
		if !touched {
			t.Fatal("expected changed activity to update in memory")
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("Touch waited for activity persistence")
	}

	session, ok := manager.Get(token)
	if !ok || session.LastSeenIP != "198.51.100.42" {
		t.Fatalf("expected in-memory activity to remain available while persistence is blocked, got %+v", session)
	}

	releaseWrite()
	waitForActivitySignal(t, store.finished, "activity persistence to finish")
}

func TestTouchKeepsSessionAvailableWhenActivityPersistenceFails(t *testing.T) {
	store := newActivitySessionStore(nil, errors.New("activity writer unavailable"))
	manager := auth.NewPersistentSessionManager(time.Hour, store)
	token, _, err := manager.CreateWithSourceAndMetadata(auth.SessionSourceStandard, auth.SessionClientMetadata{IP: "203.0.113.9"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	if !manager.Touch(token, "198.51.100.42") {
		t.Fatal("expected changed activity to update in memory")
	}
	waitForActivitySignal(t, store.finished, "failed activity persistence to finish")

	session, ok := manager.Get(token)
	if !ok || session.LastSeenIP != "198.51.100.42" {
		t.Fatalf("expected persistence failure not to invalidate the session, got %+v", session)
	}
}

func waitForActivitySignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}

type activitySessionStore struct {
	release  <-chan struct{}
	err      error
	started  chan struct{}
	finished chan struct{}
}

func newActivitySessionStore(release <-chan struct{}, err error) *activitySessionStore {
	return &activitySessionStore{
		release:  release,
		err:      err,
		started:  make(chan struct{}, 1),
		finished: make(chan struct{}, 1),
	}
}

func (s *activitySessionStore) Save(string, auth.Session) error {
	return nil
}

func (s *activitySessionStore) Get(string) (auth.Session, bool, error) {
	return auth.Session{}, false, nil
}

func (s *activitySessionStore) List(time.Time) ([]auth.SessionRecord, error) {
	return nil, nil
}

func (s *activitySessionStore) UpdateActivity(string, string, time.Time) error {
	s.started <- struct{}{}
	if s.release != nil {
		<-s.release
	}
	s.finished <- struct{}{}
	return s.err
}

func (s *activitySessionStore) UpdateAdminAliasByTokenHash(string, string, time.Time) (int64, error) {
	return 0, nil
}

func (s *activitySessionStore) Delete(string) error {
	return nil
}

func (s *activitySessionStore) DeleteByTokenHash(string) (int64, error) {
	return 0, nil
}

func (s *activitySessionStore) DeleteByRole(auth.Role) (int64, error) {
	return 0, nil
}

func (s *activitySessionStore) DeleteExpired(time.Time) error {
	return nil
}
