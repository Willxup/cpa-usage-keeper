package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type authFilesManagementClientStub struct {
	mu              sync.Mutex
	statusCalls     []authFilesManagementStatusCall
	deleteNames     []string
	statusErrByName map[string]error
	active          int
	maxActive       int
	delay           time.Duration
}

type authFilesManagementStatusCall struct {
	name     string
	disabled bool
}

func (s *authFilesManagementClientStub) UpdateAuthFileStatus(ctx context.Context, name string, disabled bool) error {
	s.mu.Lock()
	s.statusCalls = append(s.statusCalls, authFilesManagementStatusCall{name: name, disabled: disabled})
	s.active++
	if s.active > s.maxActive {
		s.maxActive = s.active
	}
	s.mu.Unlock()

	if s.delay > 0 {
		time.Sleep(s.delay)
	}

	s.mu.Lock()
	s.active--
	err := s.statusErrByName[name]
	s.mu.Unlock()
	return err
}

func (s *authFilesManagementClientStub) DeleteAuthFiles(ctx context.Context, names []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleteNames = append([]string(nil), names...)
	return nil
}

func TestAuthFilesManagementServiceDisablesWithFiveWorkers(t *testing.T) {
	client := &authFilesManagementClientStub{delay: 10 * time.Millisecond}
	service := NewAuthFilesManagementService(client)
	names := []string{"a.json", "b.json", "c.json", "d.json", "e.json", "f.json", "g.json"}

	response, err := service.SetAuthFilesDisabled(context.Background(), names, true)
	if err != nil {
		t.Fatalf("SetAuthFilesDisabled returned error: %v", err)
	}

	if response.Affected != len(names) {
		t.Fatalf("expected affected=%d, got %+v", len(names), response)
	}
	if client.maxActive > 10 {
		t.Fatalf("expected at most 10 concurrent status updates, got %d", client.maxActive)
	}
	if client.maxActive <= 1 {
		t.Fatalf("expected status updates to run concurrently, got maxActive=%d", client.maxActive)
	}
	if len(client.statusCalls) != len(names) {
		t.Fatalf("expected one status call per name, got %+v", client.statusCalls)
	}
	for _, call := range client.statusCalls {
		if !call.disabled {
			t.Fatalf("expected disabled=true for all calls, got %+v", client.statusCalls)
		}
	}
}

func TestAuthFilesManagementServiceTrimsAndDedupesNames(t *testing.T) {
	client := &authFilesManagementClientStub{}
	service := NewAuthFilesManagementService(client)

	response, err := service.DeleteAuthFiles(context.Background(), []string{" a.json ", "a.json", "b.json"})
	if err != nil {
		t.Fatalf("DeleteAuthFiles returned error: %v", err)
	}

	if strings.Join(response.Names, ",") != "a.json,b.json" || strings.Join(client.deleteNames, ",") != "a.json,b.json" {
		t.Fatalf("expected trimmed unique names, response=%+v client=%+v", response, client.deleteNames)
	}
}

func TestAuthFilesManagementServiceRejectsEmptyNames(t *testing.T) {
	client := &authFilesManagementClientStub{}
	service := NewAuthFilesManagementService(client)

	_, err := service.DeleteAuthFiles(context.Background(), []string{" "})
	if !errors.Is(err, ErrAuthFilesManagementValidation) {
		t.Fatalf("expected validation error, got %v", err)
	}

	_, err = service.SetAuthFilesDisabled(context.Background(), nil, true)
	if !errors.Is(err, ErrAuthFilesManagementValidation) {
		t.Fatalf("expected validation error, got %v", err)
	}
}

func TestAuthFilesManagementServiceReturnsStatusUpdateErrors(t *testing.T) {
	client := &authFilesManagementClientStub{statusErrByName: map[string]error{"b.json": errors.New("upstream rejected")}}
	service := NewAuthFilesManagementService(client)

	_, err := service.SetAuthFilesDisabled(context.Background(), []string{"a.json", "b.json"}, true)
	if err == nil || !strings.Contains(err.Error(), "b.json") {
		t.Fatalf("expected named status update error, got %v", err)
	}
}

func TestJoinAuthFilesManagementErrorDedupesContextCancellation(t *testing.T) {
	var joined error

	joined = joinAuthFilesManagementError(joined, context.Canceled)
	joined = joinAuthFilesManagementError(joined, context.Canceled)

	if !errors.Is(joined, context.Canceled) {
		t.Fatalf("expected joined error to contain context cancellation, got %v", joined)
	}
	if strings.Count(joined.Error(), context.Canceled.Error()) != 1 {
		t.Fatalf("expected context cancellation to appear once, got %q", joined.Error())
	}
}

func TestJoinAuthFilesManagementErrorReturnsFirstErrorDirectly(t *testing.T) {
	first := errors.New("first failure")

	if joined := joinAuthFilesManagementError(nil, first); joined != first {
		t.Fatalf("expected first error to be returned directly, got %T %[1]v", joined)
	}
}
