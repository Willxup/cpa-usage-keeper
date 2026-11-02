package test

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"cpa-usage-keeper/internal/cpa/dto/credentials"
	"cpa-usage-keeper/internal/cpa/dto/response"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/service"
)

// 模拟 CPA 对凭证先取副本、再整条替换的真实写入合同。
type credentialReplacementClient struct {
	priorityClientStub
	mu            sync.Mutex
	file          credentials.Credential
	firstKind     string
	firstStarted  chan struct{}
	releaseFirst  chan struct{}
	secondStarted chan struct{}
}

func (c *credentialReplacementClient) patch(kind string, priority int, disabled bool) (int, error) {
	c.mu.Lock()
	snapshot := c.file
	c.mu.Unlock()
	if kind == c.firstKind {
		close(c.firstStarted)
		<-c.releaseFirst
	} else {
		close(c.secondStarted)
	}
	if kind == "priority" {
		snapshot.Priority = &priority
	} else {
		snapshot.Disabled = &disabled
	}
	c.mu.Lock()
	c.file = snapshot
	c.mu.Unlock()
	return http.StatusOK, nil
}
func (c *credentialReplacementClient) UpdateCredentialPriority(_ context.Context, _ string, priority int) (int, error) {
	return c.patch("priority", priority, false)
}
func (c *credentialReplacementClient) UpdateCredentialStatus(_ context.Context, _, _ string, disabled bool) (int, error) {
	return c.patch("status", 0, disabled)
}
func (c *credentialReplacementClient) FetchProviderKeyConfig(context.Context, string) (*response.ProviderKeyConfigResult, error) {
	return nil, fmt.Errorf("unused")
}
func (c *credentialReplacementClient) DeleteCredentials(context.Context, []string) error {
	return fmt.Errorf("unused")
}

func TestCredentialStatusAndPriorityShareMutationLock(t *testing.T) {
	for _, scenario := range []struct {
		name      string
		firstKind string
		bulk      bool
	}{
		{"single-status-first", "status", false}, {"single-priority-first", "priority", false},
		{"bulk-status-first", "status", true}, {"bulk-priority-first", "priority", true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			firstKind := scenario.firstKind
			db := openMetadataTestDatabase(t, "credential-shared-lock.db")
			seedCredentialCredential(t, db, "auth.json", "auth-index")
			client := &credentialReplacementClient{file: credentials.Credential{Name: "auth.json", AuthIndex: "auth-index"}, firstKind: firstKind, firstStarted: make(chan struct{}), releaseFirst: make(chan struct{}), secondStarted: make(chan struct{})}
			locks := &service.CredentialMutationLocks{}
			status := service.NewCredentialStatusService(db, client, nil, locks, nil)
			priority := service.NewCredentialPriorityService(db, client, nil, locks, nil)
			bulk := service.NewCredentialsManagementService(client, nil, locks, nil)
			actions := map[string]func() error{
				"status": func() error {
					if scenario.bulk {
						_, err := bulk.SetCredentialsDisabled(context.Background(), []string{"auth.json"}, true)
						return err
					}
					_, err := status.SetCredentialDisabled(context.Background(), "auth-index", true)
					return err
				},
				"priority": func() error {
					_, err := priority.SetCredentialPriority(context.Background(), "auth-index", 8)
					return err
				},
			}
			secondKind := "priority"
			if firstKind == "priority" {
				secondKind = "status"
			}
			results := make(chan error, 2)
			release := sync.OnceFunc(func() { close(client.releaseFirst) })
			defer release()
			go func() { results <- actions[firstKind]() }()
			select {
			case <-client.firstStarted:
			case <-time.After(2 * time.Second):
				t.Fatal("first write did not start")
			}
			go func() { results <- actions[secondKind]() }()
			select {
			case <-client.secondStarted:
				t.Error("second operation reached CPA while the first credential replacement was pending")
			case <-time.After(100 * time.Millisecond):
			}
			release()
			for range 2 {
				select {
				case err := <-results:
					if err != nil {
						t.Errorf("update: %v", err)
					}
				case <-time.After(2 * time.Second):
					t.Fatal("update did not complete")
				}
			}
			client.mu.Lock()
			file := client.file
			client.mu.Unlock()
			if file.Priority == nil || *file.Priority != 8 || file.Disabled == nil || !*file.Disabled {
				t.Errorf("CPA lost a field update: priority=%v disabled=%v", file.Priority, file.Disabled)
			}
			requirePriority(t, loadPriority(t, db, entities.UsageIdentityAuthTypeCredential, "auth-index"), 8)
		})
	}
}
