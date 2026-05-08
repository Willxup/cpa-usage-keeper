package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"cpa-usage-keeper/internal/updatecheck"
	"github.com/gin-gonic/gin"
)

type updateCheckerStub struct {
	result updatecheck.Result
	err    error
}

func (s updateCheckerStub) Check(context.Context) (updatecheck.Result, error) {
	return s.result, s.err
}

func TestUpdateCheckReturnsResult(t *testing.T) {
	router := gin.New()
	registerUpdateRoutes(router.Group("/api/v1"), updateCheckerStub{result: updatecheck.Result{
		CurrentVersion:  "v1.2.3",
		LatestVersion:   "v1.2.4",
		UpdateAvailable: true,
		CanCompare:      true,
		Message:         "new version available: v1.2.4",
	}})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/update/check", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	body := resp.Body.String()
	if !contains(body, `"currentVersion":"v1.2.3"`) || !contains(body, `"latestVersion":"v1.2.4"`) || !contains(body, `"updateAvailable":true`) || !contains(body, `"canCompare":true`) {
		t.Fatalf("unexpected response body: %s", body)
	}
}

func TestUpdateCheckReturnsInternalError(t *testing.T) {
	router := gin.New()
	registerUpdateRoutes(router.Group("/api/v1"), updateCheckerStub{err: errors.New("network unavailable")})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/update/check", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", resp.Code)
	}
	if body := resp.Body.String(); !contains(body, `"error":"internal server error"`) {
		t.Fatalf("unexpected response body: %s", body)
	}
}
