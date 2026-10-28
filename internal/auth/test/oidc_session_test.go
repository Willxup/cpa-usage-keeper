package test

import (
	"testing"
	"time"

	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/entities"
)

func TestOIDCSessionPersistenceAliasAndCurrentPolicy(t *testing.T) {
	db := openSessionDatabase(t)
	policy := auth.OIDCSessionPolicy{Enabled: true, Issuer: "http://issuer.example/realm", ClientID: "keeper", AllowedSubjects: []string{"User,A"}}
	manager := auth.NewPersistentSessionManager(time.Hour, auth.NewGormSessionStore(db))
	manager.SetOIDCPolicy(policy)
	token, _, err := manager.CreateOIDC(policy.Issuer, "User,A", policy.ClientID, "Initial name", auth.SessionClientMetadata{IP: "192.0.2.1", UserAgent: "browser"})
	if err != nil {
		t.Fatal(err)
	}
	standard, _, _ := manager.Create()
	embed, _, _ := manager.CreateWithSource(auth.SessionSourceEmbed)
	restarted := auth.NewPersistentSessionManager(time.Hour, auth.NewGormSessionStore(db))
	restarted.SetOIDCPolicy(policy)
	session, ok := restarted.Get(token)
	if !ok || session.Source != auth.SessionSourceOIDC || session.OIDCIssuer != policy.Issuer || session.OIDCSubject != "User,A" || session.OIDCClientID != policy.ClientID || session.Alias != "Initial name" {
		t.Fatalf("persisted identity = %+v, %v", session, ok)
	}
	if !restarted.UpdateAdminAliasByTokenHash(auth.SessionTokenHash(token), "New alias") {
		t.Fatal("alias update failed")
	}
	session, _ = restarted.Get(token)
	if session.Alias != "New alias" || session.OIDCSubject != "User,A" || session.OIDCIssuer != policy.Issuer {
		t.Fatalf("alias changed identity: %+v", session)
	}
	for name, changed := range map[string]auth.OIDCSessionPolicy{
		"disabled": {}, "issuer": {Enabled: true, Issuer: policy.Issuer + "/", ClientID: policy.ClientID},
		"client":  {Enabled: true, Issuer: policy.Issuer, ClientID: "other"},
		"subject": {Enabled: true, Issuer: policy.Issuer, ClientID: policy.ClientID, AllowedSubjects: []string{"user,A"}},
	} {
		t.Run(name, func(t *testing.T) {
			// 每种策略都需要新的有效会话，避免前一个用例的删除让后续检查空跑。
			_, _, err := manager.CreateOIDC(policy.Issuer, "User,A", policy.ClientID, "", auth.SessionClientMetadata{})
			if err != nil {
				t.Fatal(err)
			}
			check := auth.NewPersistentSessionManager(time.Hour, auth.NewGormSessionStore(db))
			check.SetOIDCPolicy(changed)
			// 列表与业务鉴权采用同一策略；旧普通/嵌入会话不依赖 OIDC 配置。
			for _, record := range check.List() {
				if record.Source == auth.SessionSourceOIDC {
					t.Fatal("invalid OIDC session listed")
				}
			}
			if check.Validate(standard) != true || check.Validate(embed) != true {
				t.Fatal("OIDC policy affected existing login sources")
			}
		})
	}
	restarted.SetOIDCPolicy(auth.OIDCSessionPolicy{})
	if restarted.Validate(token) {
		t.Fatal("disabled OIDC session still authenticated")
	}
}

func TestOIDCMissingIdentityAndUnknownSourcesNeverBecomeStandard(t *testing.T) {
	db := openSessionDatabase(t)
	manager := auth.NewPersistentSessionManager(time.Hour, auth.NewGormSessionStore(db))
	manager.SetOIDCPolicy(auth.OIDCSessionPolicy{Enabled: true, Issuer: "issuer", ClientID: "keeper"})
	for _, source := range []string{"oidc", "future-source"} {
		token := "invalid-" + source
		row := entities.AuthSession{TokenHash: auth.SessionTokenHash(token), Role: "admin", Source: source, ExpiresAt: time.Now().Add(time.Hour), CreatedAt: time.Now()}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
		if manager.Validate(token) {
			t.Fatalf("invalid source %s authenticated", source)
		}
	}
}
