package quota

import (
	"cpa-usage-keeper/internal/cpa"
	"cpa-usage-keeper/internal/entities"
	"net/http"
	"testing"
	"time"
)

func TestTrafficSelectionDeduplicatesAndValidatesCurrentAccounts(t *testing.T) {
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	item := cpa.QuotaObservation{AuthIndex: "synthetic", Provider: "codex", Window: "primary", ObservedAt: at, Headers: http.Header{"X-Codex-Primary-Used-Percent": {"12"}, "X-Codex-Primary-Window-Minutes": {"300"}, "X-Codex-Primary-Reset-After-Seconds": {"120"}}}
	snapshot, ok := BuildUsageHeaderSnapshot(UsageHeaderSnapshotInput{AuthType: "oauth", AuthIndex: item.AuthIndex, Provider: item.Provider, ObservedAt: at, Headers: item.Headers})
	if !ok {
		t.Fatal("no snapshot")
	}
	identity := entities.UsageIdentity{Identity: item.AuthIndex, Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile}
	identities := map[string]entities.UsageIdentity{item.AuthIndex: identity}
	selected, seen := selectTrafficQuotaSnapshots([]cpa.QuotaObservation{item}, []*UsageHeaderSnapshot{snapshot}, identities, nil)
	if len(selected) != 1 {
		t.Fatal("initial observation lost")
	}
	selected, seen = selectTrafficQuotaSnapshots([]cpa.QuotaObservation{item}, []*UsageHeaderSnapshot{snapshot}, identities, seen)
	if len(selected) != 0 || len(seen) != 1 {
		t.Fatal("repeated observation requeued")
	}
	for _, changed := range []entities.UsageIdentity{
		{Identity: item.AuthIndex, Type: "claude", AuthType: entities.UsageIdentityAuthTypeAuthFile},
		{Identity: item.AuthIndex, Type: "codex", AuthType: entities.UsageIdentityAuthTypeAIProvider},
		{Identity: item.AuthIndex, Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile, IsDeleted: true},
		{Identity: item.AuthIndex, Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile, Disabled: new(true)},
	} {
		selected, _ = selectTrafficQuotaSnapshots([]cpa.QuotaObservation{item}, []*UsageHeaderSnapshot{snapshot}, map[string]entities.UsageIdentity{item.AuthIndex: changed}, nil)
		if len(selected) != 0 {
			t.Fatal("inactive/wrong provider accepted")
		}
	}
	selected, seen = selectTrafficQuotaSnapshots(nil, nil, nil, seen)
	if len(selected) != 0 || len(seen) != 0 {
		t.Fatal("removed observations retained")
	}
}
