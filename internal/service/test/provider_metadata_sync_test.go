package test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"cpa-usage-keeper/internal/cpa/dto/authfiles"
	"cpa-usage-keeper/internal/cpa/dto/providerconfig"
	"cpa-usage-keeper/internal/cpa/dto/response"
	"cpa-usage-keeper/internal/entities"
	"gorm.io/gorm"
)

func TestProviderMetadataSyncPreservesSourceFields(t *testing.T) {
	db := openMetadataTestDatabase(t, "existing-provider-fields.db")
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	priority := 7
	disabled := true
	note := "provider note"
	fetcher := newMetadataTestFetcher()
	fetcher.standardResults["codex"] = &response.ProviderKeyConfigResult{StatusCode: 200, Payload: []providerconfig.ProviderKeyConfig{{APIKey: "secret-codex", AuthIndex: "auth-codex", Prefix: "prefix-codex", BaseURL: "https://codex.example/v1", Name: "Codex Team", Priority: &priority, Disabled: &disabled, Note: &note}}}
	fetcher.standardResults["xai"] = &response.ProviderKeyConfigResult{StatusCode: 200, Payload: []providerconfig.ProviderKeyConfig{{APIKey: "secret-xai", AuthIndex: "auth-xai", Prefix: "prefix-xai", BaseURL: "https://xai.example/v1"}}}
	fetcher.standardResults["gemini"] = &response.ProviderKeyConfigResult{StatusCode: 200, Payload: []providerconfig.ProviderKeyConfig{{APIKey: "secret-gemini", AuthIndex: "auth-gemini", Prefix: "prefix-gemini", BaseURL: "https://gemini.example/v1"}}}
	fetcher.standardResults["gemini-interactions"] = &response.ProviderKeyConfigResult{StatusCode: 200, Payload: []providerconfig.ProviderKeyConfig{{APIKey: "secret-interactions", AuthIndex: "auth-interactions", Prefix: "prefix-interactions", BaseURL: "https://interactions.example/v1"}}}
	fetcher.standardResults["claude"] = &response.ProviderKeyConfigResult{StatusCode: 200, Payload: []providerconfig.ProviderKeyConfig{{APIKey: "secret-claude", AuthIndex: "auth-claude", Prefix: "prefix-claude", BaseURL: "https://claude.example/v1", Name: "Claude Team"}}}
	fetcher.standardResults["vertex"] = &response.ProviderKeyConfigResult{StatusCode: 200, Payload: []providerconfig.ProviderKeyConfig{{APIKey: "secret-vertex", AuthIndex: "auth-vertex", Prefix: "prefix-vertex", BaseURL: "https://vertex.example/v1", Name: "Vertex Team"}}}
	fetcher.standardResults["meta"] = &response.ProviderKeyConfigResult{StatusCode: 200, Payload: []providerconfig.ProviderKeyConfig{{APIKey: "secret-meta", AuthIndex: "auth-meta", Prefix: "prefix-meta", BaseURL: "https://meta.example/v1", Priority: &priority, Disabled: &disabled, Note: &note}}}
	fetcher.openAIResult = &response.OpenAICompatibilityResult{StatusCode: 200, Payload: []providerconfig.OpenAICompatibilityConfig{{Name: "OpenRouter", Prefix: "prefix-openai", BaseURL: "https://openrouter.example/v1", Priority: &priority, Disabled: &disabled, Note: &note, APIKeyEntries: []providerconfig.OpenAIApiKeyEntry{{APIKey: "secret-openai-a", AuthIndex: "auth-openai-a"}, {APIKey: "secret-openai-b", AuthIndex: "auth-openai-b"}}}}}
	syncer := newMetadataTestSyncer(db, fetcher, func() time.Time { return now })
	if err := syncer.SyncMetadata(context.Background()); err != nil {
		t.Fatalf("SyncMetadata returned error: %v", err)
	}
	identities := loadMetadataIdentityMap(t, db)
	codexRow := identities[metadataIdentityKey(entities.UsageIdentityAuthTypeAIProvider, "auth-codex")]
	// Identity 只来自 auth-index，LookupKey 只来自 api-key，其它字段互不推导。
	if codexRow.Name != "Codex Team" || codexRow.Provider != "Codex Team" || codexRow.Identity != "auth-codex" || codexRow.LookupKey != "secret-codex" || codexRow.Prefix != "prefix-codex" || codexRow.BaseURL != "https://codex.example/v1" || codexRow.Type != "codex" || codexRow.AuthTypeName != "apikey" || codexRow.IsDeleted {
		t.Fatalf("codex provider identity = %+v", codexRow)
	}
	if codexRow.Priority == nil || *codexRow.Priority != priority || codexRow.Disabled == nil || *codexRow.Disabled != disabled || codexRow.Note == nil || *codexRow.Note != note {
		t.Fatalf("codex provider optional metadata = %+v", codexRow)
	}
	xAIRow := identities[metadataIdentityKey(entities.UsageIdentityAuthTypeAIProvider, "auth-xai")]
	if xAIRow.Name != "xAI" || xAIRow.Provider != "xAI" || xAIRow.Type != "xai" || xAIRow.Identity != "auth-xai" || xAIRow.LookupKey != "secret-xai" || xAIRow.Prefix != "prefix-xai" || xAIRow.BaseURL != "https://xai.example/v1" {
		t.Fatalf("xAI provider identity = %+v", xAIRow)
	}
	geminiRow := identities[metadataIdentityKey(entities.UsageIdentityAuthTypeAIProvider, "auth-gemini")]
	if geminiRow.Name != "gemini" || geminiRow.Provider != "gemini" || geminiRow.LookupKey != "secret-gemini" || geminiRow.Prefix != "prefix-gemini" || geminiRow.BaseURL != "https://gemini.example/v1" || geminiRow.Type != "gemini" {
		t.Fatalf("gemini provider identity = %+v", geminiRow)
	}
	interactionsRow := identities[metadataIdentityKey(entities.UsageIdentityAuthTypeAIProvider, "auth-interactions")]
	if interactionsRow.Name != "Gemini Interactions" || interactionsRow.Provider != "Gemini Interactions" || interactionsRow.Type != "gemini-interactions" || interactionsRow.Identity != "auth-interactions" || interactionsRow.LookupKey != "secret-interactions" || interactionsRow.Prefix != "prefix-interactions" || interactionsRow.BaseURL != "https://interactions.example/v1" {
		t.Fatalf("Gemini Interactions provider identity = %+v", interactionsRow)
	}
	claudeRow := identities[metadataIdentityKey(entities.UsageIdentityAuthTypeAIProvider, "auth-claude")]
	if claudeRow.Type != "claude" || claudeRow.Name != "Claude Team" || claudeRow.LookupKey != "secret-claude" {
		t.Fatalf("claude provider identity = %+v", claudeRow)
	}
	vertexRow := identities[metadataIdentityKey(entities.UsageIdentityAuthTypeAIProvider, "auth-vertex")]
	if vertexRow.Type != "vertex" || vertexRow.Name != "Vertex Team" || vertexRow.LookupKey != "secret-vertex" {
		t.Fatalf("vertex provider identity = %+v", vertexRow)
	}
	metaRow := identities[metadataIdentityKey(entities.UsageIdentityAuthTypeAIProvider, "auth-meta")]
	if metaRow.Type != "meta" || metaRow.Name != "Meta" || metaRow.Provider != "Meta" || metaRow.Identity != "auth-meta" || metaRow.LookupKey != "secret-meta" || metaRow.Prefix != "prefix-meta" || metaRow.BaseURL != "https://meta.example/v1" || metaRow.IsDeleted || metaRow.Priority == nil || *metaRow.Priority != priority || metaRow.Disabled == nil || *metaRow.Disabled != disabled || metaRow.Note == nil || *metaRow.Note != note {
		t.Fatalf("meta provider identity = %+v", metaRow)
	}
	openAIFirst := identities[metadataIdentityKey(entities.UsageIdentityAuthTypeAIProvider, "auth-openai-a")]
	if openAIFirst.Type != "openai" || openAIFirst.Name != "OpenRouter" || openAIFirst.Provider != "OpenRouter" || openAIFirst.LookupKey != "secret-openai-a" || openAIFirst.Prefix != "prefix-openai" || openAIFirst.BaseURL != "https://openrouter.example/v1" {
		t.Fatalf("first OpenAI compatibility identity = %+v", openAIFirst)
	}
	openAISecond := identities[metadataIdentityKey(entities.UsageIdentityAuthTypeAIProvider, "auth-openai-b")]
	if openAISecond.LookupKey != "secret-openai-b" || openAISecond.Identity != "auth-openai-b" || openAISecond.Prefix != "prefix-openai" || openAISecond.BaseURL != "https://openrouter.example/v1" {
		t.Fatalf("second OpenAI compatibility identity = %+v", openAISecond)
	}
	if _, ok := identities[metadataIdentityKey(entities.UsageIdentityAuthTypeAIProvider, "prefix-codex")]; ok {
		t.Fatalf("provider prefix created an identity: %+v", identities)
	}
	for _, source := range []string{"codex", "xai", "gemini", "gemini-interactions", "claude", "vertex", "meta", "openai"} {
		if fetcher.callCount(source) != 1 {
			t.Fatalf("%s calls = %d", source, fetcher.callCount(source))
		}
	}
}

func TestProviderMetadataSyncKeepsFailedSourcesAndStalesOnlySuccessfulTypes(t *testing.T) {
	db := openMetadataTestDatabase(t, "provider-stale-boundaries.db")
	oldTime := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	now := oldTime.Add(24 * time.Hour)
	seed := []entities.UsageIdentity{
		{Name: "Old Gemini", AuthType: entities.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "old-gemini", Type: "gemini", Provider: "Gemini", CreatedAt: oldTime, UpdatedAt: oldTime},
		{Name: "Old Meta", AuthType: entities.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "old-meta", Type: "meta", Provider: "Meta", LookupKey: "old-meta-secret", CreatedAt: oldTime, UpdatedAt: oldTime},
		{Name: "Old Claude", AuthType: entities.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "old-claude", Type: "claude", Provider: "Claude", CreatedAt: oldTime, UpdatedAt: oldTime},
		{Name: "Old Codex", AuthType: entities.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "old-codex", Type: "codex", Provider: "Codex", CreatedAt: oldTime, UpdatedAt: oldTime},
		{Name: "Old Vertex", AuthType: entities.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "old-vertex", Type: "vertex", Provider: "Vertex", CreatedAt: oldTime, UpdatedAt: oldTime},
	}
	if err := db.Create(&seed).Error; err != nil {
		t.Fatalf("seed provider identities: %v", err)
	}
	fetcher := newMetadataTestFetcher()
	fetcher.standardResults["gemini"] = nil
	fetcher.standardErrors["gemini"] = errors.New("gemini unavailable")
	fetcher.standardResults["meta"] = nil
	fetcher.standardErrors["meta"] = errors.New("meta unavailable")
	fetcher.standardResults["claude"] = &response.ProviderKeyConfigResult{StatusCode: 200, Payload: []providerconfig.ProviderKeyConfig{}}
	fetcher.standardResults["codex"] = nil
	fetcher.standardResults["vertex"] = &response.ProviderKeyConfigResult{StatusCode: 200, Payload: []providerconfig.ProviderKeyConfig{{APIKey: "invalid-without-auth-index"}}}
	syncer := newMetadataTestSyncer(db, fetcher, func() time.Time { return now })
	err := syncer.SyncMetadata(context.Background())
	if err == nil || !strings.Contains(err.Error(), "fetch gemini api keys: gemini unavailable") || !strings.Contains(err.Error(), "codex api keys response is nil") {
		t.Fatalf("provider boundary warning = %v", err)
	}
	identities := loadMetadataIdentityMap(t, db)
	geminiRow := identities[metadataIdentityKey(entities.UsageIdentityAuthTypeAIProvider, "old-gemini")]
	if geminiRow.IsDeleted || geminiRow.DeletedAt != nil || !geminiRow.UpdatedAt.Equal(oldTime) {
		t.Fatalf("failed Gemini identity = %+v", geminiRow)
	}
	metaRow := identities[metadataIdentityKey(entities.UsageIdentityAuthTypeAIProvider, "old-meta")]
	if metaRow.IsDeleted || metaRow.DeletedAt != nil || !metaRow.UpdatedAt.Equal(oldTime) {
		t.Fatalf("failed Meta identity = %+v", metaRow)
	}
	codexRow := identities[metadataIdentityKey(entities.UsageIdentityAuthTypeAIProvider, "old-codex")]
	if codexRow.IsDeleted || codexRow.DeletedAt != nil || !codexRow.UpdatedAt.Equal(oldTime) {
		t.Fatalf("nil Codex identity = %+v", codexRow)
	}
	claudeRow := identities[metadataIdentityKey(entities.UsageIdentityAuthTypeAIProvider, "old-claude")]
	if !claudeRow.IsDeleted || claudeRow.DeletedAt == nil || !claudeRow.DeletedAt.Equal(now) || !claudeRow.UpdatedAt.Equal(now) {
		t.Fatalf("empty Claude identity = %+v", claudeRow)
	}
	vertexRow := identities[metadataIdentityKey(entities.UsageIdentityAuthTypeAIProvider, "old-vertex")]
	if !vertexRow.IsDeleted || vertexRow.DeletedAt == nil || !vertexRow.DeletedAt.Equal(now) || !vertexRow.UpdatedAt.Equal(now) {
		t.Fatalf("invalid Vertex identity = %+v", vertexRow)
	}
}

func TestProviderMetadataSyncNewSourcesTreatOnlyTyped404AsOptional(t *testing.T) {
	db := openMetadataTestDatabase(t, "new-provider-optional-404.db")
	oldTime := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	firstNow := oldTime.Add(24 * time.Hour)
	secondNow := firstNow.Add(time.Hour)
	// seed 同时包含相同 auth-index 的 xAI OAuth 与 xAI API Key，关联只能依赖 auth_type + identity。
	seed := []entities.UsageIdentity{
		{Name: "xAI OAuth", AuthType: entities.UsageIdentityAuthTypeAuthFile, AuthTypeName: "oauth", Identity: "shared-xai-auth", Type: "xai", Provider: "xAI", CreatedAt: oldTime, UpdatedAt: oldTime},
		{Name: "xAI API Key", AuthType: entities.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "shared-xai-auth", Type: "xai", Provider: "xAI", LookupKey: "old-xai-secret", CreatedAt: oldTime, UpdatedAt: oldTime},
		{Name: "Interactions", AuthType: entities.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "old-interactions", Type: "gemini-interactions", Provider: "Gemini Interactions", LookupKey: "old-interactions-secret", CreatedAt: oldTime, UpdatedAt: oldTime},
		{Name: "Meta", AuthType: entities.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "old-meta", Type: "meta", Provider: "Meta", LookupKey: "old-meta-secret", CreatedAt: oldTime, UpdatedAt: oldTime},
	}
	if err := db.Create(&seed).Error; err != nil {
		t.Fatalf("seed new provider identities: %v", err)
	}
	fetcher := newMetadataTestFetcher()
	fetcher.setAuthFiles([]authfiles.AuthFile{{AuthIndex: "shared-xai-auth", Type: "xai", Provider: "xAI", Name: "xai.json"}})
	fetcher.standardResults["xai"] = &response.ProviderKeyConfigResult{StatusCode: 404}
	fetcher.standardErrors["xai"] = errors.New("xai endpoint missing")
	fetcher.standardResults["gemini-interactions"] = &response.ProviderKeyConfigResult{StatusCode: 404}
	fetcher.standardErrors["gemini-interactions"] = errors.New("interactions endpoint missing")
	fetcher.standardResults["meta"] = &response.ProviderKeyConfigResult{StatusCode: 404}
	fetcher.standardErrors["meta"] = errors.New("meta endpoint missing")
	currentNow := firstNow
	syncer := newMetadataTestSyncer(db, fetcher, func() time.Time { return currentNow })
	if err := syncer.SyncMetadata(context.Background()); err != nil {
		t.Fatalf("typed 404 SyncMetadata returned error: %v", err)
	}
	firstRows := loadMetadataIdentityMap(t, db)
	xAIProvider := firstRows[metadataIdentityKey(entities.UsageIdentityAuthTypeAIProvider, "shared-xai-auth")]
	if xAIProvider.IsDeleted || xAIProvider.DeletedAt != nil || !xAIProvider.UpdatedAt.Equal(oldTime) {
		t.Fatalf("xAI provider after typed 404 = %+v", xAIProvider)
	}
	interactions := firstRows[metadataIdentityKey(entities.UsageIdentityAuthTypeAIProvider, "old-interactions")]
	if interactions.IsDeleted || interactions.DeletedAt != nil || !interactions.UpdatedAt.Equal(oldTime) {
		t.Fatalf("Interactions after typed 404 = %+v", interactions)
	}
	metaProvider := firstRows[metadataIdentityKey(entities.UsageIdentityAuthTypeAIProvider, "old-meta")]
	if metaProvider.IsDeleted || metaProvider.DeletedAt != nil || !metaProvider.UpdatedAt.Equal(oldTime) {
		t.Fatalf("Meta provider after typed 404 = %+v", metaProvider)
	}
	fetcher.standardResults["xai"] = &response.ProviderKeyConfigResult{StatusCode: 200, Payload: []providerconfig.ProviderKeyConfig{}}
	fetcher.standardErrors["xai"] = nil
	fetcher.standardResults["gemini-interactions"] = &response.ProviderKeyConfigResult{StatusCode: 200, Payload: []providerconfig.ProviderKeyConfig{}}
	fetcher.standardErrors["gemini-interactions"] = nil
	fetcher.standardResults["meta"] = &response.ProviderKeyConfigResult{StatusCode: 200, Payload: []providerconfig.ProviderKeyConfig{}}
	fetcher.standardErrors["meta"] = nil
	currentNow = secondNow
	if err := syncer.SyncMetadata(context.Background()); err != nil {
		t.Fatalf("empty-list SyncMetadata returned error: %v", err)
	}
	secondRows := loadMetadataIdentityMap(t, db)
	xAIProvider = secondRows[metadataIdentityKey(entities.UsageIdentityAuthTypeAIProvider, "shared-xai-auth")]
	if !xAIProvider.IsDeleted || xAIProvider.DeletedAt == nil || !xAIProvider.DeletedAt.Equal(secondNow) || !xAIProvider.UpdatedAt.Equal(secondNow) {
		t.Fatalf("xAI provider after empty list = %+v", xAIProvider)
	}
	interactions = secondRows[metadataIdentityKey(entities.UsageIdentityAuthTypeAIProvider, "old-interactions")]
	if !interactions.IsDeleted || interactions.DeletedAt == nil || !interactions.DeletedAt.Equal(secondNow) || !interactions.UpdatedAt.Equal(secondNow) {
		t.Fatalf("Interactions after empty list = %+v", interactions)
	}
	metaProvider = secondRows[metadataIdentityKey(entities.UsageIdentityAuthTypeAIProvider, "old-meta")]
	if !metaProvider.IsDeleted || metaProvider.DeletedAt == nil || !metaProvider.DeletedAt.Equal(secondNow) || !metaProvider.UpdatedAt.Equal(secondNow) {
		t.Fatalf("Meta provider after empty list = %+v", metaProvider)
	}
	xAIOAuth := secondRows[metadataIdentityKey(entities.UsageIdentityAuthTypeAuthFile, "shared-xai-auth")]
	// provider stale 不能跨 auth_type 删除 OAuth 行；相同 Auth File metadata 不刷新时间。
	if xAIOAuth.IsDeleted || xAIOAuth.DeletedAt != nil || !xAIOAuth.UpdatedAt.Equal(firstNow) {
		t.Fatalf("xAI OAuth after provider stale = %+v", xAIOAuth)
	}
}

// providerPersistenceProjection 只比较业务字段，不把数据库自增 ID 当作 metadata 等价条件。
type providerPersistenceProjection struct {
	Name         string
	ProviderType string
	Provider     string
	LookupKey    string
	Prefix       string
	BaseURL      string
	IsDeleted    bool
}

func TestProviderMetadataSyncCompletionOrderDoesNotChangeDatabase(t *testing.T) {
	registryOrder := []string{"codex", "xai", "gemini", "gemini-interactions", "claude", "vertex", "meta", "openai"}
	orders := []struct {
		name            string
		completionOrder []string
	}{
		{name: "forward", completionOrder: []string{"codex", "xai", "gemini", "gemini-interactions", "claude", "vertex", "meta", "openai"}},
		{name: "reverse", completionOrder: []string{"openai", "meta", "vertex", "claude", "gemini-interactions", "gemini", "xai", "codex"}},
		{name: "mixed", completionOrder: []string{"gemini", "openai", "codex", "claude", "xai", "meta", "vertex", "gemini-interactions"}},
	}
	want := make(map[string]providerPersistenceProjection, len(registryOrder))
	for _, source := range registryOrder {
		want["auth-"+source] = providerPersistenceProjection{
			Name: "name-" + source, ProviderType: source, Provider: "name-" + source,
			LookupKey: "secret-" + source, Prefix: "prefix-" + source, BaseURL: "https://" + source + ".example/v1",
		}
	}
	for _, order := range orders {
		t.Run(order.name, func(t *testing.T) {
			db := openMetadataTestDatabase(t, "completion-"+order.name+".db")
			fetcher := newMetadataTestFetcher()
			entered := make(chan string, len(registryOrder))
			done := make(chan string, len(registryOrder))
			gates := make(map[string]chan struct{}, len(registryOrder))
			gateOnce := make(map[string]*sync.Once, len(registryOrder))
			for _, source := range registryOrder {
				gates[source] = make(chan struct{})
				gateOnce[source] = &sync.Once{}
			}
			release := func(source string) {
				gateOnce[source].Do(func() {
					close(gates[source])
				})
			}
			// 失败路径也释放全部 endpoint，避免 goroutine 泄漏。
			t.Cleanup(func() {
				for _, source := range registryOrder {
					release(source)
				}
			})
			for _, source := range registryOrder[:len(registryOrder)-1] {
				fetcher.standardHooks[source] = func(ctx context.Context) (*response.ProviderKeyConfigResult, error) {
					entered <- source
					select {
					case <-gates[source]:
					case <-ctx.Done():
						return nil, ctx.Err()
					}
					done <- source
					return &response.ProviderKeyConfigResult{StatusCode: 200, Payload: []providerconfig.ProviderKeyConfig{{APIKey: "secret-" + source, AuthIndex: "auth-" + source, Name: "name-" + source, Prefix: "prefix-" + source, BaseURL: "https://" + source + ".example/v1"}}}, nil
				}
			}
			fetcher.openAIHook = func(ctx context.Context) (*response.OpenAICompatibilityResult, error) {
				entered <- "openai"
				select {
				case <-gates["openai"]:
				case <-ctx.Done():
					return nil, ctx.Err()
				}
				done <- "openai"
				return &response.OpenAICompatibilityResult{StatusCode: 200, Payload: []providerconfig.OpenAICompatibilityConfig{{Name: "name-openai", Prefix: "prefix-openai", BaseURL: "https://openai.example/v1", APIKeyEntries: []providerconfig.OpenAIApiKeyEntry{{APIKey: "secret-openai", AuthIndex: "auth-openai"}}}}}, nil
			}
			now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
			syncer := newMetadataTestSyncer(db, fetcher, func() time.Time { return now })
			resultCh := make(chan error, 1)
			go func() {
				resultCh <- syncer.SyncMetadata(context.Background())
			}()
			waitForMetadataSourceSet(t, entered, registryOrder)
			for _, source := range order.completionOrder {
				release(source)
				waitForMetadataSourceSet(t, done, []string{source})
			}
			select {
			case err := <-resultCh:
				if err != nil {
					t.Fatalf("%s SyncMetadata returned error: %v", order.name, err)
				}
			case <-time.After(time.Second):
				t.Fatal("timed out waiting for completion-order SyncMetadata")
			}
			projection := loadProviderPersistenceProjection(t, db)
			if !reflect.DeepEqual(projection, want) {
				t.Fatalf("%s projection = %#v, want %#v", order.name, projection, want)
			}
		})
	}
}

func waitForMetadataSourceSet(t *testing.T, events <-chan string, want []string) {
	t.Helper()
	seen := make(map[string]struct{}, len(want))
	// timer 防止串行回归或 goroutine 泄漏永久阻塞测试。
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for len(seen) < len(want) {
		select {
		case source := <-events:
			seen[source] = struct{}{}
		case <-timer.C:
			t.Fatalf("timed out waiting for metadata sources: got=%v want=%v", seen, want)
		}
	}
	for _, source := range want {
		if _, ok := seen[source]; !ok {
			t.Fatalf("metadata source %q did not run: %v", source, seen)
		}
	}
}

func loadProviderPersistenceProjection(t *testing.T, db *gorm.DB) map[string]providerPersistenceProjection {
	t.Helper()
	var rows []entities.UsageIdentity
	if err := db.Where("auth_type = ?", entities.UsageIdentityAuthTypeAIProvider).Find(&rows).Error; err != nil {
		t.Fatalf("load provider persistence projection: %v", err)
	}
	projection := make(map[string]providerPersistenceProjection, len(rows))
	for _, row := range rows {
		projection[row.Identity] = providerPersistenceProjection{Name: row.Name, ProviderType: row.Type, Provider: row.Provider, LookupKey: row.LookupKey, Prefix: row.Prefix, BaseURL: row.BaseURL, IsDeleted: row.IsDeleted}
	}
	return projection
}
