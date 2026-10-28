package test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	keeperservice "cpa-usage-keeper/internal/service"
)

type credentialsManagementClientStub struct {
	mu              sync.Mutex
	statusCalls     []credentialsManagementStatusCall
	deleteNames     []string
	statusErrByName map[string]error
	active          int
	maxActive       int
	delay           time.Duration
}

type credentialsManagementStatusCall struct {
	name     string
	disabled bool
}

func (s *credentialsManagementClientStub) UpdateCredentialStatus(ctx context.Context, name string, authIndex string, disabled bool) (int, error) {
	s.mu.Lock()
	s.statusCalls = append(s.statusCalls, credentialsManagementStatusCall{name: name, disabled: disabled})
	s.active++
	if s.active > s.maxActive {
		s.maxActive = s.active
	}
	s.mu.Unlock()

	time.Sleep(s.delay)

	s.mu.Lock()
	s.active--
	err := s.statusErrByName[name]
	s.mu.Unlock()
	return http.StatusOK, err
}

func (s *credentialsManagementClientStub) DeleteCredentials(ctx context.Context, names []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleteNames = append([]string(nil), names...)
	return nil
}

func TestCredentialsManagementServiceBoundsConcurrentStatusUpdates(t *testing.T) {
	// 虚拟时间等到全部 worker 阻塞后再推进，避免真实 sleep 和调度运气。
	synctest.Test(t, func(t *testing.T) {
		client := &credentialsManagementClientStub{delay: 10 * time.Millisecond}
		service := keeperservice.NewCredentialsManagementService(client, nil, &keeperservice.CredentialMutationLocks{}, nil)
		names := make([]string, 20)
		for i := range names {
			names[i] = fmt.Sprintf("auth-%d.json", i)
		}

		response, err := service.SetCredentialsDisabled(context.Background(), names, true)
		if err != nil {
			t.Fatalf("SetCredentialsDisabled returned error: %v", err)
		}

		if response.Affected != len(names) {
			t.Fatalf("expected affected=%d, got %+v", len(names), response)
		}
		if client.maxActive != 10 {
			t.Fatalf("expected 10 concurrent status updates, got %d", client.maxActive)
		}
		if len(client.statusCalls) != len(names) {
			t.Fatalf("expected one status call per name, got %+v", client.statusCalls)
		}
		for _, call := range client.statusCalls {
			if !call.disabled {
				t.Fatalf("expected disabled=true for all calls, got %+v", client.statusCalls)
			}
		}
	})
}

func TestCredentialsManagementServiceTrimsAndDedupesNames(t *testing.T) {
	client := &credentialsManagementClientStub{}
	service := keeperservice.NewCredentialsManagementService(client, nil, &keeperservice.CredentialMutationLocks{}, nil)

	response, err := service.DeleteCredentials(context.Background(), []string{" a.json ", "a.json", "b.json"})
	if err != nil {
		t.Fatalf("DeleteCredentials returned error: %v", err)
	}

	if !slices.Equal(response.Names, []string{"a.json", "b.json"}) || !slices.Equal(client.deleteNames, []string{"a.json", "b.json"}) {
		t.Fatalf("expected trimmed unique names, response=%+v client=%+v", response, client.deleteNames)
	}
}

func TestCredentialsManagementServiceRejectsEmptyNames(t *testing.T) {
	client := &credentialsManagementClientStub{}
	service := keeperservice.NewCredentialsManagementService(client, nil, &keeperservice.CredentialMutationLocks{}, nil)

	_, err := service.DeleteCredentials(context.Background(), []string{" "})
	if !errors.Is(err, keeperservice.ErrCredentialsManagementValidation) {
		t.Fatalf("expected validation error, got %v", err)
	}

	_, err = service.SetCredentialsDisabled(context.Background(), nil, true)
	if !errors.Is(err, keeperservice.ErrCredentialsManagementValidation) {
		t.Fatalf("expected validation error, got %v", err)
	}
}

func TestCredentialsManagementServiceReturnsStatusUpdateErrors(t *testing.T) {
	client := &credentialsManagementClientStub{statusErrByName: map[string]error{"b.json": errors.New("upstream rejected")}}
	service := keeperservice.NewCredentialsManagementService(client, nil, &keeperservice.CredentialMutationLocks{}, nil)

	_, err := service.SetCredentialsDisabled(context.Background(), []string{"a.json", "b.json"}, true)
	if err == nil || !strings.Contains(err.Error(), "b.json") {
		t.Fatalf("expected named status update error, got %v", err)
	}
}
