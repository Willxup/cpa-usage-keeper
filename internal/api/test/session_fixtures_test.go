package test

import (
	"net/http"
	"time"

	keeperapi "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/auth"
)

// managedSessionTrustedProxyCIDRs mirrors the Docker gateway and CDN peers CPA sits behind.
var managedSessionTrustedProxyCIDRs = []string{"172.17.0.0/16", "172.64.0.0/13"}

func newManagedSessionRouter(manager *auth.SessionManager) http.Handler {
	return newManagedSessionRouterWithTrustedProxies(manager, nil)
}

func newManagedSessionRouterWithTrustedProxies(manager *auth.SessionManager, trustedProxyCIDRs []string) http.Handler {
	config := keeperapi.AuthConfig{
		Enabled:           true,
		LoginPassword:     "secret",
		SessionTTL:        time.Hour,
		TrustedProxyCIDRs: trustedProxyCIDRs,
	}
	return keeperapi.NewRouter(nil, nil, nil, nil, config, keeperapi.NewAuthHandler(config, manager), "")
}
