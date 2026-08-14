package api

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/helper"
	"cpa-usage-keeper/internal/timeutil"
	"github.com/gin-gonic/gin"
)

const (
	authSessionKindAdmin  = "admin"
	authSessionKindAPIKey = "api_key"
	authSessionTimeLayout = "2006/01/02 15:04:05"
)

type authSessionListResponse struct {
	Items []authSessionItemResponse `json:"items"`
}

type authSessionItemResponse struct {
	ID          string    `json:"id"`
	Kind        string    `json:"kind"`
	Role        auth.Role `json:"role"`
	Source      string    `json:"source"`
	Current     bool      `json:"current,omitempty"`
	LoginAt     string    `json:"loginAt,omitempty"`
	LastSeenAt  string    `json:"lastSeenAt,omitempty"`
	ExpiresAt   string    `json:"expiresAt,omitempty"`
	LoginIP     string    `json:"loginIp,omitempty"`
	LastSeenIP  string    `json:"lastSeenIp,omitempty"`
	UserAgent   string    `json:"userAgent,omitempty"`
	APIKeyID    string    `json:"apiKeyId,omitempty"`
	Label       string    `json:"label,omitempty"`
	DisplayKey  string    `json:"displayKey,omitempty"`
	sortSeenAt  time.Time `json:"-"`
	sortLoginAt time.Time `json:"-"`
}

func registerAuthSessionManagementRoutes(router gin.IRoutes, handler *authHandler) {
	router.GET("/auth/sessions", handler.listManagedSessions)
	router.DELETE("/auth/sessions/:id", handler.revokeManagedSession)
}

func (h *authHandler) listManagedSessions(c *gin.Context) {
	if h == nil || !h.config.Enabled || h.sessions == nil {
		c.JSON(http.StatusOK, authSessionListResponse{Items: []authSessionItemResponse{}})
		return
	}
	records := h.sessions.List()
	apiKeysByID, ok := h.sessionAPIKeysByID(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, authSessionListResponse{Items: buildAuthSessionItems(records, apiKeysByID, currentAuthSessionHash(c))})
}

func (h *authHandler) revokeManagedSession(c *gin.Context) {
	if h == nil || !h.config.Enabled || h.sessions == nil {
		c.Status(http.StatusNoContent)
		return
	}
	sessionID := strings.TrimSpace(c.Param("id"))
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid session id"})
		return
	}
	result := h.sessions.DeleteByTokenHash(sessionID)
	if result.Deleted == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}
	h.clearSessionStateForTokens(result.Tokens)
	if sessionID == currentAuthSessionHash(c) {
		clearSessionCookie(c, h.config.BasePath, resolveSessionToken(c).CookieKind)
	}
	c.Status(http.StatusNoContent)
}

func (h *authHandler) sessionAPIKeysByID(c *gin.Context) (map[int64]entities.CPAAPIKey, bool) {
	rowsByID := map[int64]entities.CPAAPIKey{}
	if h == nil || h.cpaAPIKeyProvider == nil {
		return rowsByID, true
	}
	rows, err := h.cpaAPIKeyProvider.ListCPAAPIKeys(c.Request.Context())
	if err != nil {
		writeInternalError(c, "list api keys for auth sessions failed", err)
		return nil, false
	}
	for _, row := range rows {
		rowsByID[row.ID] = row
	}
	return rowsByID, true
}

func buildAuthSessionItems(records []auth.SessionRecord, apiKeysByID map[int64]entities.CPAAPIKey, currentTokenHash string) []authSessionItemResponse {
	items := make([]authSessionItemResponse, 0, len(records))

	for _, record := range records {
		base := authSessionItemResponse{
			ID: record.TokenHash, Role: record.Role, Source: string(auth.NormalizeSessionSource(record.Source)),
			Current: record.TokenHash == currentTokenHash, LoginAt: formatAuthSessionTime(record.CreatedAt),
			LastSeenAt: formatAuthSessionTime(record.LastSeenAt), ExpiresAt: formatAuthSessionTime(record.ExpiresAt),
			LoginIP: record.LoginIP, LastSeenIP: record.LastSeenIP, UserAgent: record.UserAgent,
			sortSeenAt: record.LastSeenAt, sortLoginAt: record.CreatedAt,
		}
		if record.Role == auth.RoleAdmin {
			base.Kind = authSessionKindAdmin
			items = append(items, base)
			continue
		}
		if record.Role != auth.RoleAPIKeyViewer {
			continue
		}
		label, displayKey := apiKeySessionDisplay(record.CPAAPIKeyID, apiKeysByID)
		base.Kind = authSessionKindAPIKey
		base.APIKeyID = strconv.FormatInt(record.CPAAPIKeyID, 10)
		base.Label = label
		base.DisplayKey = displayKey
		items = append(items, base)
	}

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Current != items[j].Current {
			return items[i].Current
		}
		if !items[i].sortSeenAt.Equal(items[j].sortSeenAt) {
			return items[i].sortSeenAt.After(items[j].sortSeenAt)
		}
		if !items[i].sortLoginAt.Equal(items[j].sortLoginAt) {
			return items[i].sortLoginAt.After(items[j].sortLoginAt)
		}
		if items[i].ID != items[j].ID {
			return items[i].ID < items[j].ID
		}
		return false
	})
	return items
}

func currentAuthSessionHash(c *gin.Context) string {
	if value, ok := c.Get("auth_token"); ok {
		if token, ok := value.(string); ok && token != "" {
			return auth.SessionTokenHash(token)
		}
	}
	resolved := resolveSessionToken(c)
	if resolved.Token == "" {
		return ""
	}
	return auth.SessionTokenHash(resolved.Token)
}

func formatAuthSessionTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return timeutil.NormalizeStorageTime(value).Format(authSessionTimeLayout)
}

func apiKeySessionDisplay(apiKeyID int64, apiKeysByID map[int64]entities.CPAAPIKey) (string, string) {
	if row, ok := apiKeysByID[apiKeyID]; ok {
		return helper.CPAAPIKeyDisplayName(row), helper.CPAAPIKeyMaskedDisplayKey(row)
	}
	fallback := fmt.Sprintf("Unknown API Key #%d", apiKeyID)
	return fallback, fallback
}
