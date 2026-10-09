package test

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/quota"
)

func TestHeaderProviderSwitchSurvivesOldRefreshCompletion(t *testing.T) {
	for _, tc := range []struct {
		name   string
		queued bool
		err    error
	}{
		{name: "running_success"}, {name: "running_failure", err: errors.New("upstream failed")},
		{name: "running_unsupported", err: quota.ErrUnsupportedType}, {name: "queued", queued: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := openQuotaTestDatabase(t)
			seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "shared-auth", Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile})
			entered := make(chan struct{})
			unblock := make(chan struct{})
			release := sync.OnceFunc(func() { close(unblock) })
			handler := &refreshHandlerStub{block: unblock, err: tc.err, onCheck: func() { close(entered) }, output: quota.ProviderOutput{Provider: "codex", Result: quota.CodexResult{}}}
			service := newQuotaServiceWithRegistryAndOptions(t, db, quota.NewProviderRegistry(map[string]quota.ProviderHandler{"codex": handler, "claude": handler}), quota.ServiceOptions{RefreshWorkerLimit: 1})
			finished := make(chan struct{}, 1)
			setRefreshCooldown(service, func(time.Duration) { finished <- struct{}{} })
			t.Cleanup(release)
			releaseSlot := func() {}
			if tc.queued {
				releaseSlot = sync.OnceFunc(occupyRefreshWorkerToken(service))
				t.Cleanup(releaseSlot)
			}
			queueManualQuotaRefresh(t, service, "shared-auth")
			if !tc.queued {
				select {
				case <-entered:
				case <-time.After(3 * time.Second):
					t.Fatal("refresh did not start")
				}
			}
			if err := db.Model(&entities.UsageIdentity{}).Where("identity = ?", "shared-auth").Updates(map[string]any{"provider": "claude", "type": "claude"}).Error; err != nil {
				t.Fatal(err)
			}
			now := time.Now().Truncate(time.Second)
			snapshot, ok := quota.BuildUsageHeaderSnapshot(quota.UsageHeaderSnapshotInput{AuthType: "oauth", AuthIndex: "shared-auth", Provider: "claude", ObservedAt: now, Headers: http.Header{
				"Anthropic-Ratelimit-Unified-5h-Utilization": {"0.25"},
				"Anthropic-Ratelimit-Unified-5h-Reset":       {strconv.FormatInt(now.Add(5*time.Hour).Unix(), 10)},
			}})
			if !ok || !applyUsageHeaderSnapshot(service, context.Background(), *snapshot) {
				t.Fatal("new provider header blocked")
			}
			release()
			releaseSlot()
			select {
			case <-finished:
			case <-time.After(3 * time.Second):
				t.Fatal("old worker did not finish")
			}
			service.StopRefreshTasks()
			cached, err := service.GetRefreshTaskByAuthIndex(context.Background(), "shared-auth")
			if err != nil || cached.Status != quota.RefreshTaskStatusCompleted || cached.Quota == nil || len(cached.Quota.Quota) != 1 || cached.Quota.Quota[0].Key != "five_hour" {
				t.Fatalf("old task changed new provider cache: %+v err=%v", cached, err)
			}
			if tc.queued && handler.callCount() != 0 {
				t.Fatal("obsolete queued task invoked provider")
			}
		})
	}
}
