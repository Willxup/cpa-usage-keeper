package test

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"

	"cpa-usage-keeper/internal/cpa/dto/providerconfig"
	"cpa-usage-keeper/internal/cpa/dto/response"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/service"
	"gorm.io/gorm"
)

type priorityClientStub struct {
	providers       map[string][]providerconfig.ProviderKeyConfig
	openAI          []providerconfig.OpenAICompatibilityConfig
	writeGroup      int
	writeType       string
	patchName       string
	patchStatus     int
	providerFetches int
	openAIFetches   int
	onOpenAIFetch   func(int)
	onPatch         func()
}

func (s *priorityClientStub) UpdateCredentialPriority(_ context.Context, name string, priority int) (int, error) {
	s.patchName = name
	if s.patchStatus != 0 {
		return s.patchStatus, errors.New("upstream rejected")
	}
	if s.onPatch != nil {
		s.onPatch()
	}
	return http.StatusOK, nil
}

func (s *priorityClientStub) FetchPriorityProviderConfig(_ context.Context, providerType string) (*response.ProviderKeyConfigResult, error) {
	s.providerFetches++
	return &response.ProviderKeyConfigResult{StatusCode: http.StatusOK, Payload: s.providers[providerType], Document: testProviderDocument(s.providers[providerType])}, nil
}

func (s *priorityClientStub) UpdateProviderConfig(_ context.Context, providerType string, document *providerconfig.Document, _ string) (int, error) {
	s.writeType = providerType
	if providerType == "openai" {
		before := testOpenAIDocument(s.openAI)
		for index := range document.Groups {
			if !reflect.DeepEqual(before.Groups[index].Fields["priority"], document.Groups[index].Fields["priority"]) {
				s.writeGroup = index
				break
			}
		}
	} else {
		before := testProviderDocument(s.providers[providerType])
		for index := range document.Groups {
			if !reflect.DeepEqual(before.Groups[index].Keys[0].Fields["priority"], document.Groups[index].Keys[0].Fields["priority"]) {
				s.writeGroup = index
				break
			}
		}
	}
	if s.patchStatus != 0 {
		return s.patchStatus, errors.New("upstream rejected")
	}
	if s.onPatch != nil {
		s.onPatch()
	}
	return http.StatusOK, nil
}

func (s *priorityClientStub) FetchOpenAICompatibility(context.Context) (*response.OpenAICompatibilityResult, error) {
	s.openAIFetches++
	if s.onOpenAIFetch != nil {
		s.onOpenAIFetch(s.openAIFetches)
	}
	return &response.OpenAICompatibilityResult{StatusCode: http.StatusOK, Payload: s.openAI, Document: testOpenAIDocument(s.openAI)}, nil
}

func loadPriority(t *testing.T, db *gorm.DB, authType entities.UsageIdentityAuthType, identity string) *int {
	t.Helper()
	var row entities.UsageIdentity
	if err := db.Where("auth_type = ? AND identity = ?", authType, identity).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	return row.Priority
}

func requirePriority(t *testing.T, got *int, want int) {
	t.Helper()
	if got == nil || *got != want {
		t.Fatalf("priority=%v, want %d", got, want)
	}
}

func TestCredentialPriorityPersistsExplicitZero(t *testing.T) {
	db := openMetadataTestDatabase(t, "priority-credential-zero.db")
	seedCredentialCredential(t, db, "auth.json", "auth-index")
	client := &priorityClientStub{}
	refresher := &credentialStatusRefresherStub{}
	provider := service.NewCredentialPriorityService(db, client, refresher, &service.CredentialMutationLocks{}, nil)
	result, err := provider.SetCredentialPriority(context.Background(), "auth-index", 0)
	if err != nil || result.Priority != 0 || client.patchName != "auth.json" {
		t.Fatalf("result=%+v err=%v patch=%q", result, err, client.patchName)
	}
	requirePriority(t, loadPriority(t, db, entities.UsageIdentityAuthTypeCredential, "auth-index"), 0)
	if refresher.count() != 1 {
		t.Fatalf("refresh count=%d", refresher.count())
	}
}

func TestCredentialPriorityRejectsUpstreamConflict(t *testing.T) {
	db := openMetadataTestDatabase(t, "priority-credential-conflict.db")
	seedCredentialCredential(t, db, "auth.json", "auth-index")
	client := &priorityClientStub{patchStatus: http.StatusConflict}
	provider := service.NewCredentialPriorityService(db, client, nil, &service.CredentialMutationLocks{}, nil)
	_, err := provider.SetCredentialPriority(context.Background(), "auth-index", 3)
	if !errors.Is(err, service.ErrCredentialPriorityConflict) {
		t.Fatalf("err=%v, want conflict", err)
	}
	if got := loadPriority(t, db, entities.UsageIdentityAuthTypeCredential, "auth-index"); got != nil {
		t.Fatalf("unexpected local priority=%v", *got)
	}
}

func TestCredentialPriorityPersistsSuccessfulPatch(t *testing.T) {
	db := openMetadataTestDatabase(t, "priority-credential-patch-success.db")
	seedCredentialCredential(t, db, "auth.json", "auth-index")
	client := &priorityClientStub{}
	refresher := &credentialStatusRefresherStub{}
	result, err := service.NewCredentialPriorityService(db, client, refresher, &service.CredentialMutationLocks{}, nil).SetCredentialPriority(context.Background(), "auth-index", 3)
	if err != nil || result.Priority != 3 || refresher.count() != 1 {
		t.Fatalf("result=%+v err=%v refresh=%d", result, err, refresher.count())
	}
	requirePriority(t, loadPriority(t, db, entities.UsageIdentityAuthTypeCredential, "auth-index"), 3)
}

func TestProviderPriorityUsesFirstAuthIndexMatchAndNegativeValue(t *testing.T) {
	db := openMetadataTestDatabase(t, "priority-provider-index.db")
	seedProviderCredential(t, db, "meta", "target", "same-secret")
	client := &priorityClientStub{providers: map[string][]providerconfig.ProviderKeyConfig{
		"meta": {{AuthIndex: "other", APIKey: "same-secret"}, {AuthIndex: "target", APIKey: "same-secret"}},
	}}
	provider := service.NewCredentialPriorityService(db, client, nil, &service.CredentialMutationLocks{}, nil)
	if _, err := provider.SetAIProviderPriority(context.Background(), "target", -8); err != nil {
		t.Fatal(err)
	}
	if client.writeType != "meta" || client.writeGroup != 1 {
		t.Fatalf("patch type=%s index=%d", client.writeType, client.writeGroup)
	}
	requirePriority(t, loadPriority(t, db, entities.UsageIdentityAuthTypeAIProvider, "target"), -8)
}

func TestProviderPriorityPersistsExplicitZero(t *testing.T) {
	db := openMetadataTestDatabase(t, "priority-provider-zero.db")
	seedProviderCredential(t, db, "vertex", "target", "secret")
	client := &priorityClientStub{providers: map[string][]providerconfig.ProviderKeyConfig{"vertex": {{AuthIndex: "target"}}}}
	result, err := service.NewCredentialPriorityService(db, client, nil, &service.CredentialMutationLocks{}, nil).SetAIProviderPriority(context.Background(), "target", 0)
	if err != nil || result.Priority != 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	requirePriority(t, loadPriority(t, db, entities.UsageIdentityAuthTypeAIProvider, "target"), 0)
}

func TestProviderPriorityDoesNotPersistMissingOrRejectedTarget(t *testing.T) {
	for _, tc := range []struct {
		name    string
		entries []providerconfig.ProviderKeyConfig
		status  int
		want    error
	}{
		{"deleted", []providerconfig.ProviderKeyConfig{{AuthIndex: "another"}}, 0, service.ErrCredentialPriorityNotFound},
		{"rejected", []providerconfig.ProviderKeyConfig{{AuthIndex: "target"}}, http.StatusConflict, service.ErrCredentialPriorityConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := openMetadataTestDatabase(t, "priority-provider-"+tc.name+".db")
			seedProviderCredential(t, db, "codex", "target", "secret")
			client := &priorityClientStub{providers: map[string][]providerconfig.ProviderKeyConfig{"codex": tc.entries}, patchStatus: tc.status}
			_, err := service.NewCredentialPriorityService(db, client, nil, &service.CredentialMutationLocks{}, nil).SetAIProviderPriority(context.Background(), "target", 10)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err=%v, want %v", err, tc.want)
			}
			if got := loadPriority(t, db, entities.UsageIdentityAuthTypeAIProvider, "target"); got != nil {
				t.Fatalf("unexpected local priority=%v", *got)
			}
		})
	}
}

func TestProviderPriorityPersistsTargetPUT(t *testing.T) {
	db := openMetadataTestDatabase(t, "priority-provider-patch-success.db")
	seedProviderCredential(t, db, "codex", "target", "secret")
	client := &priorityClientStub{providers: map[string][]providerconfig.ProviderKeyConfig{"codex": {{AuthIndex: "another"}, {AuthIndex: "target"}}}}
	result, err := service.NewCredentialPriorityService(db, client, nil, &service.CredentialMutationLocks{}, nil).SetAIProviderPriority(context.Background(), "target", -4)
	if err != nil || result.Priority != -4 || client.providerFetches != 1 || client.writeGroup != 1 {
		t.Fatalf("result=%+v err=%v fetches=%d index=%d", result, err, client.providerFetches, client.writeGroup)
	}
	requirePriority(t, loadPriority(t, db, entities.UsageIdentityAuthTypeAIProvider, "target"), -4)
}

func TestOpenAIProviderPriorityUpdatesAllItsKeysOnly(t *testing.T) {
	db := openMetadataTestDatabase(t, "priority-openai-group.db")
	for _, authIndex := range []string{"openai-a", "openai-b", "other-provider"} {
		seedProviderCredential(t, db, "openai", authIndex, "secret-"+authIndex)
	}
	client := &priorityClientStub{openAI: []providerconfig.OpenAICompatibilityConfig{
		{Name: "other", APIKeyEntries: []providerconfig.OpenAIApiKeyEntry{{AuthIndex: "other-provider"}}},
		{Name: "target", APIKeyEntries: []providerconfig.OpenAIApiKeyEntry{{AuthIndex: "openai-a"}, {AuthIndex: "openai-b"}}},
	}}
	result, err := service.NewCredentialPriorityService(db, client, nil, &service.CredentialMutationLocks{}, nil).SetAIProviderPriority(context.Background(), "openai-b", 12)
	if err != nil || result.Priority != 12 || client.writeGroup != 1 {
		t.Fatalf("result=%+v err=%v index=%d", result, err, client.writeGroup)
	}
	for _, authIndex := range []string{"openai-a", "openai-b"} {
		requirePriority(t, loadPriority(t, db, entities.UsageIdentityAuthTypeAIProvider, authIndex), 12)
	}
	if got := loadPriority(t, db, entities.UsageIdentityAuthTypeAIProvider, "other-provider"); got != nil {
		t.Fatalf("other provider priority=%v", *got)
	}
}

func TestOpenAIProviderPriorityUsesCurrentGroupKeys(t *testing.T) {
	db := openMetadataTestDatabase(t, "priority-openai-patch-success.db")
	for _, authIndex := range []string{"a", "b", "old"} {
		seedProviderCredential(t, db, "openai", authIndex, "secret-"+authIndex)
	}
	client := &priorityClientStub{openAI: []providerconfig.OpenAICompatibilityConfig{{APIKeyEntries: []providerconfig.OpenAIApiKeyEntry{{AuthIndex: "a"}, {AuthIndex: "old"}}}}}
	client.onOpenAIFetch = func(fetch int) {
		if fetch == 1 {
			client.openAI[0].APIKeyEntries = []providerconfig.OpenAIApiKeyEntry{{AuthIndex: "a"}, {AuthIndex: "b"}}
		}
	}
	result, err := service.NewCredentialPriorityService(db, client, nil, &service.CredentialMutationLocks{}, nil).SetAIProviderPriority(context.Background(), "a", 6)
	if err != nil || result.Priority != 6 || client.openAIFetches != 1 || client.writeGroup != 0 {
		t.Fatalf("result=%+v err=%v fetches=%d index=%d", result, err, client.openAIFetches, client.writeGroup)
	}
	for _, authIndex := range []string{"a", "b"} {
		requirePriority(t, loadPriority(t, db, entities.UsageIdentityAuthTypeAIProvider, authIndex), 6)
	}
	if got := loadPriority(t, db, entities.UsageIdentityAuthTypeAIProvider, "old"); got != nil {
		t.Fatalf("unrelated key priority=%v", *got)
	}
}

func TestOpenAIProviderPriorityUnknownKeyDoesNotWrite(t *testing.T) {
	db := openMetadataTestDatabase(t, "priority-openai-missing.db")
	seedProviderCredential(t, db, "openai", "missing", "secret")
	client := &priorityClientStub{openAI: []providerconfig.OpenAICompatibilityConfig{{APIKeyEntries: []providerconfig.OpenAIApiKeyEntry{{AuthIndex: "someone-else"}}}}}
	_, err := service.NewCredentialPriorityService(db, client, nil, &service.CredentialMutationLocks{}, nil).SetAIProviderPriority(context.Background(), "missing", 5)
	if !errors.Is(err, service.ErrCredentialPriorityNotFound) || client.writeType != "" {
		t.Fatalf("err=%v patch=%s", err, client.writeType)
	}
}

func TestOpenAIProviderPriorityRejectedPUTDoesNotPersist(t *testing.T) {
	db := openMetadataTestDatabase(t, "priority-openai-rejected.db")
	seedProviderCredential(t, db, "openai", "target", "secret")
	client := &priorityClientStub{
		openAI:      []providerconfig.OpenAICompatibilityConfig{{APIKeyEntries: []providerconfig.OpenAIApiKeyEntry{{AuthIndex: "target"}}}},
		patchStatus: http.StatusConflict,
	}
	_, err := service.NewCredentialPriorityService(db, client, nil, &service.CredentialMutationLocks{}, nil).SetAIProviderPriority(context.Background(), "target", 5)
	if !errors.Is(err, service.ErrCredentialPriorityConflict) || client.openAIFetches != 1 {
		t.Fatalf("err=%v fetches=%d", err, client.openAIFetches)
	}
	if got := loadPriority(t, db, entities.UsageIdentityAuthTypeAIProvider, "target"); got != nil {
		t.Fatalf("unexpected local priority=%v", *got)
	}
}

func TestPriorityRequestsRefreshIfLocalPersistenceFailsAfterCPASuccess(t *testing.T) {
	db := openMetadataTestDatabase(t, "priority-local-failure.db")
	seedProviderCredential(t, db, "gemini", "target", "secret")
	refresher := &credentialStatusRefresherStub{}
	client := &priorityClientStub{providers: map[string][]providerconfig.ProviderKeyConfig{"gemini": {{AuthIndex: "target"}}}}
	client.onPatch = func() {
		if err := db.Migrator().DropTable(&entities.UsageIdentity{}); err != nil {
			t.Fatal(err)
		}
	}
	_, err := service.NewCredentialPriorityService(db, client, refresher, &service.CredentialMutationLocks{}, nil).SetAIProviderPriority(context.Background(), "target", 4)
	if err == nil || refresher.count() != 1 {
		t.Fatalf("err=%v refresh=%d", err, refresher.count())
	}
}

func TestPriorityUnsupportedTypeDoesNotPatch(t *testing.T) {
	db := openMetadataTestDatabase(t, "priority-unsupported.db")
	seedProviderCredential(t, db, "unknown", "target", "secret")
	client := &priorityClientStub{}
	_, err := service.NewCredentialPriorityService(db, client, nil, &service.CredentialMutationLocks{}, nil).SetAIProviderPriority(context.Background(), "target", 1)
	if !errors.Is(err, service.ErrCredentialPriorityUnsupported) {
		t.Fatalf("err=%v", err)
	}
	if client.writeType != "" {
		t.Fatalf("unexpected patch=%s", client.writeType)
	}
}

var _ service.CredentialPriorityClient = (*priorityClientStub)(nil)
