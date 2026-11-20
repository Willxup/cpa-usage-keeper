package test

import (
	"context"
	"testing"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/service"
)

func TestMetaAuthFileManagementUsesCredentialPathOnly(t *testing.T) {
	db := openMetadataTestDatabase(t, "meta-credential-status.db")
	name := "meta-account.json"
	seedCredentialStatusIdentity(t, db, entities.UsageIdentity{AuthType: entities.UsageIdentityAuthTypeCredential, Identity: "shared-meta", Type: "meta", Provider: "meta", FileName: &name})
	seedCredentialStatusIdentity(t, db, entities.UsageIdentity{AuthType: entities.UsageIdentityAuthTypeAIProvider, Identity: "shared-meta", Type: "meta", Provider: "meta", Disabled: metadataBoolPointer(false)})
	client := &credentialStatusClientStub{}
	manager := service.NewCredentialStatusService(db, client, nil, &service.CredentialMutationLocks{}, nil)
	if _, err := manager.SetCredentialDisabled(context.Background(), "shared-meta", true); err != nil {
		t.Fatal(err)
	}
	if len(client.credentialCalls) != 1 || client.credentialCalls[0] != (credentialStatusCredentialCall{name: name, authIndex: "shared-meta", disabled: true}) || len(client.providerCalls) != 0 {
		t.Fatal("Meta auth-file management used the wrong CPA path")
	}
	fileStatus := loadCredentialStatusDisabled(t, db, entities.UsageIdentityAuthTypeCredential, "shared-meta")
	keyStatus := loadCredentialStatusDisabled(t, db, entities.UsageIdentityAuthTypeAIProvider, "shared-meta")
	if fileStatus == nil || !*fileStatus || keyStatus == nil || *keyStatus {
		t.Fatal("Meta auth-file status affected the API-key identity")
	}
}
