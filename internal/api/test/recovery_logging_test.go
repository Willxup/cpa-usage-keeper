package test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	. "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/logging"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

func TestRouterRecoveryUsesUnifiedKeeperLog(t *testing.T) {
	previousMode := gin.Mode()
	gin.SetMode(gin.ReleaseMode)
	t.Cleanup(func() { gin.SetMode(previousMode) })

	closer, err := logging.Configure(config.Config{LogLevel: "info"})
	if err != nil {
		t.Fatalf("configure logging: %v", err)
	}
	closed := false
	t.Cleanup(func() {
		if !closed {
			_ = closer.Close()
		}
	})

	var output bytes.Buffer
	logrus.SetOutput(&output)
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "")
	router.GET("/panic-review", func(*gin.Context) {
		panic("review-panic")
	})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/panic-review", nil)
	router.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("expected panic recovery status 500, got %d", response.Code)
	}

	if err := closer.Close(); err != nil {
		t.Fatalf("close logging: %v", err)
	}
	closed = true
	plain := regexp.MustCompile(`\x1b\[[0-9;]*m`).ReplaceAllString(output.String(), "")
	if !strings.Contains(plain, "| error | gin panic recovered |") {
		t.Fatalf("expected Logrus-native Gin recovery entry, got %q", plain)
	}
	for _, want := range []string{"method=GET", "panic=review-panic", "path=/panic-review", "stack="} {
		if !strings.Contains(plain, want) {
			t.Fatalf("expected recovery field %q, got %q", want, plain)
		}
	}
	if strings.Contains(plain, `\x1b`) || regexp.MustCompile(`\d{4}/\d{2}/\d{2}`).MatchString(plain) {
		t.Fatalf("expected no nested Gin ANSI or timestamp, got %q", plain)
	}
	if strings.Count(plain, "\n") != 1 {
		t.Fatalf("expected one physical recovery log line, got %q", plain)
	}
}
