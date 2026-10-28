package test

import (
	"context"
	"strings"
	"testing"
	"time"

	keeperapp "cpa-usage-keeper/internal/app"
	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/oidc"
)

func TestOIDCStartupRevokesDormantSessionsBeforeServing(t *testing.T) {
	for _, change := range []string{"unchanged", "secret", "disabled", "auth-disabled", "issuer", "client", "subject", "allowed-subject"} {
		t.Run(change, func(t *testing.T) {
			cfg := testAppConfig(t)
			cfg.AuthEnabled, cfg.LoginPassword = true, "password"
			cfg.OIDC = oidc.Config{Enabled: true, Issuer: "http://idp.invalid", ClientID: "keeper", ClientSecret: "secret", RedirectURL: "http://keeper.example" + cfg.AppBasePath + "/api/v1/auth/oidc/callback"}
			seed := newInitializedApp(t, cfg)
			store := auth.NewGormSessionStore(seed.DB)
			for _, source := range []auth.SessionSource{auth.SessionSourceOIDC, auth.SessionSourceStandard, auth.SessionSourceEmbed} {
				if err := store.Save(string(source), auth.Session{Source: source, Role: auth.RoleAdmin, OIDCIssuer: cfg.OIDC.Issuer, OIDCSubject: "User,A", OIDCClientID: cfg.OIDC.ClientID, CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
					t.Fatal(err)
				}
			}
			if err := seed.Close(); err != nil {
				t.Fatal(err)
			}
			switch change {
			case "secret":
				cfg.OIDC.ClientSecret = "rotated"
			case "disabled":
				cfg.OIDC.Enabled = false
			case "auth-disabled":
				cfg.OIDC.Enabled, cfg.AuthEnabled = false, false
			case "issuer":
				cfg.OIDC.Issuer += "/other"
			case "client":
				cfg.OIDC.ClientID = "other"
			case "subject":
				cfg.OIDC.AllowedSubjects = []string{"user,A"}
			case "allowed-subject":
				cfg.OIDC.AllowedSubjects = []string{"User,A"}
			}
			restarted := newInitializedApp(t, cfg)
			// 旧浏览器和管理页都没有发起请求；启动本身必须完成持久撤销。
			var rows []entities.AuthSession
			if err := restarted.DB.Find(&rows).Error; err != nil {
				t.Fatal(err)
			}
			keepOIDC := change == "unchanged" || change == "secret" || change == "allowed-subject"
			want := 2
			if keepOIDC {
				want++
			}
			if len(rows) != want {
				t.Fatalf("sessions after startup = %d, want %d", len(rows), want)
			}
			for _, row := range rows {
				if row.Source == "oidc" && !keepOIDC {
					t.Fatal("dormant OIDC session survived startup")
				}
			}
		})
	}
}

func TestOIDCStartupCleanupFailurePreventsBusinessInitialization(t *testing.T) {
	cfg := testAppConfig(t)
	cfg.AuthEnabled, cfg.LoginPassword = true, "password"
	seed := newInitializedApp(t, cfg)
	if err := auth.NewGormSessionStore(seed.DB).Save("dormant", auth.Session{Source: auth.SessionSourceOIDC, Role: auth.RoleAdmin, CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := seed.DB.Exec(`CREATE TRIGGER fail_oidc_cleanup BEFORE DELETE ON auth_sessions WHEN OLD.source = 'oidc' BEGIN SELECT RAISE(ABORT, 'cleanup unavailable'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if err := seed.Close(); err != nil {
		t.Fatal(err)
	}
	application, err := keeperapp.NewWithConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = application.Close() })
	if err := application.Initialize(context.Background()); err == nil || !strings.Contains(err.Error(), "revoke invalid OIDC sessions") {
		t.Fatalf("startup error = %v", err)
	}
	if application.Router != nil {
		t.Fatal("business router became available after failed revocation")
	}
}
