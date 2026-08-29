package test

import (
	"net/http"
	"testing"
	"time"

	keeperapp "cpa-usage-keeper/internal/app"
	"cpa-usage-keeper/internal/config"
)

func TestHTTPServerProtectsConnectionSetupWithoutLimitingAuthenticatedResponses(t *testing.T) {
	server := keeperapp.NewHTTPServer(config.Config{AppHost: "127.0.0.1", AppPort: "8080"}, http.NotFoundHandler())

	if server.ReadHeaderTimeout != 5*time.Second {
		t.Fatalf("expected five second read-header timeout, got %s", server.ReadHeaderTimeout)
	}
	if server.IdleTimeout != 60*time.Second {
		t.Fatalf("expected sixty second idle timeout, got %s", server.IdleTimeout)
	}
	if server.MaxHeaderBytes != 64<<10 {
		t.Fatalf("expected 64 KiB header limit, got %d", server.MaxHeaderBytes)
	}
	if server.ReadTimeout != 0 || server.WriteTimeout != 0 {
		t.Fatalf("expected active authenticated requests and responses to remain unrestricted, got read=%s write=%s", server.ReadTimeout, server.WriteTimeout)
	}
}
