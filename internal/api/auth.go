package api

import (
	"crypto/subtle"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/helper"
	"cpa-usage-keeper/internal/service"
	"github.com/gin-gonic/gin"
)

const sessionCookieName = "cpa_usage_keeper_session"

const maxFailedLoginAttempts = 5

type AuthConfig struct {
	Enabled       bool
	LoginPassword string
	SessionTTL    time.Duration
	BasePath      string
}

type authHandler struct {
	config            AuthConfig
	sessions          *auth.SessionManager
	cpaAPIKeyProvider service.CPAAPIKeyProvider

	mu                  sync.Mutex
	failedAttempts      map[string]int
	keyOverviewRequests map[string]time.Time
}

type loginRequest struct {
	Password string `json:"password"`
}

type apiKeyLoginRequest struct {
	APIKey string `json:"apiKey"`
}

type sessionResponse struct {
	Authenticated bool                   `json:"authenticated"`
	Role          auth.Role              `json:"role,omitempty"`
	APIKey        *sessionAPIKeyResponse `json:"api_key,omitempty"`
}

type sessionAPIKeyResponse struct {
	DisplayKey string `json:"display_key"`
	Alias      string `json:"alias,omitempty"`
}

func NewAuthHandler(config AuthConfig, sessions *auth.SessionManager) *authHandler {
	return &authHandler{config: config, sessions: sessions, failedAttempts: make(map[string]int), keyOverviewRequests: make(map[string]time.Time)}
}

func (h *authHandler) setCPAAPIKeyProvider(provider service.CPAAPIKeyProvider) {
	if h != nil {
		h.cpaAPIKeyProvider = provider
	}
}

func (h *authHandler) registerRoutes(router gin.IRoutes) {
	router.GET("/session", h.getSession)
	router.POST("/login", h.login)
	router.POST("/api-key-login", h.apiKeyLogin)
	router.POST("/logout", h.logout)
}

func (h *authHandler) middleware() gin.HandlerFunc {
	return h.roleMiddleware()
}

func (h *authHandler) adminMiddleware() gin.HandlerFunc {
	return h.roleMiddleware(auth.RoleAdmin)
}

func (h *authHandler) apiKeyViewerMiddleware() gin.HandlerFunc {
	return h.roleMiddleware(auth.RoleAPIKeyViewer)
}

func (h *authHandler) roleMiddleware(allowedRoles ...auth.Role) gin.HandlerFunc {
	return func(c *gin.Context) {
		if h == nil || !h.config.Enabled {
			c.Next()
			return
		}
		if h.sessions == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
			return
		}

		token, err := c.Cookie(sessionCookieName)
		session, ok := h.sessions.Get(token)
		if err != nil || !ok {
			h.deleteSession(token)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
			return
		}
		if len(allowedRoles) > 0 && !sessionRoleAllowed(session.Role, allowedRoles) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "forbidden"})
			return
		}
		c.Set("auth_token", token)
		c.Set("auth_session", session)
		c.Next()
	}
}

func sessionRoleAllowed(role auth.Role, allowedRoles []auth.Role) bool {
	for _, allowed := range allowedRoles {
		if role == allowed {
			return true
		}
	}
	return false
}

func (h *authHandler) getSession(c *gin.Context) {
	if h == nil || !h.config.Enabled {
		c.JSON(http.StatusOK, sessionResponse{Authenticated: true, Role: auth.RoleAdmin})
		return
	}
	if h.sessions == nil {
		c.JSON(http.StatusOK, sessionResponse{Authenticated: false})
		return
	}

	token, err := c.Cookie(sessionCookieName)
	if err != nil {
		c.JSON(http.StatusOK, sessionResponse{Authenticated: false})
		return
	}
	session, ok := h.sessions.Get(token)
	if !ok {
		h.deleteSession(token)
		c.JSON(http.StatusOK, sessionResponse{Authenticated: false})
		return
	}
	response := sessionResponse{Authenticated: true, Role: session.Role}
	if session.Role == auth.RoleAPIKeyViewer {
		row, ok := h.activeViewerAPIKey(c, token, session)
		if !ok {
			c.JSON(http.StatusOK, sessionResponse{Authenticated: false})
			return
		}
		response.APIKey = &sessionAPIKeyResponse{DisplayKey: helper.CPAAPIKeyMaskedDisplayKey(row), Alias: row.KeyAlias}
	}
	c.JSON(http.StatusOK, response)
}

func (h *authHandler) login(c *gin.Context) {
	if h == nil || !h.config.Enabled {
		c.Status(http.StatusNoContent)
		return
	}
	if h.sessions == nil {
		writeInternalError(c, "session manager is not configured", nil)
		return
	}

	var request loginRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	clientKey := loginClientKey(c)
	passwordMatches := subtle.ConstantTimeCompare([]byte(request.Password), []byte(h.config.LoginPassword)) == 1
	if h.tooManyFailedAttempts(clientKey) && !passwordMatches {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "too many failed login attempts"})
		return
	}

	if !passwordMatches {
		h.recordFailedAttempt(clientKey)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid password"})
		return
	}
	h.clearFailedAttempts(clientKey)

	token, expiresAt, err := h.sessions.Create()
	if err != nil {
		writeInternalError(c, "create auth session failed", err)
		return
	}

	setSessionCookie(c, h.config.BasePath, token, expiresAt)
	c.Status(http.StatusNoContent)
}

func (h *authHandler) apiKeyLogin(c *gin.Context) {
	if h == nil || !h.config.Enabled {
		c.Status(http.StatusNoContent)
		return
	}
	if h.sessions == nil || h.cpaAPIKeyProvider == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}
	clientKey := loginClientKey(c)
	var request apiKeyLoginRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		if h.tooManyFailedAttempts(clientKey) {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "too many failed login attempts"})
			return
		}
		h.recordFailedAttempt(clientKey)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}
	row, err := h.cpaAPIKeyProvider.FindActiveCPAAPIKeyByValue(c.Request.Context(), request.APIKey)
	if err != nil {
		if h.tooManyFailedAttempts(clientKey) {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "too many failed login attempts"})
			return
		}
		h.recordFailedAttempt(clientKey)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}
	h.clearFailedAttempts(clientKey)
	token, expiresAt, err := h.sessions.CreateAPIKeyViewer(row.ID)
	if err != nil {
		writeInternalError(c, "create api key viewer session failed", err)
		return
	}
	setSessionCookie(c, h.config.BasePath, token, expiresAt)
	c.Status(http.StatusNoContent)
}

func (h *authHandler) activeViewerAPIKey(c *gin.Context, token string, session auth.Session) (entities.CPAAPIKey, bool) {
	if h.cpaAPIKeyProvider == nil || session.CPAAPIKeyID <= 0 {
		h.deleteSession(token)
		clearSessionCookie(c, h.config.BasePath)
		return entities.CPAAPIKey{}, false
	}
	row, err := h.cpaAPIKeyProvider.FindActiveCPAAPIKeyByID(c.Request.Context(), session.CPAAPIKeyID)
	if err != nil {
		h.deleteSession(token)
		clearSessionCookie(c, h.config.BasePath)
		return entities.CPAAPIKey{}, false
	}
	return row, true
}

func (h *authHandler) logout(c *gin.Context) {
	if h == nil || !h.config.Enabled {
		c.Status(http.StatusNoContent)
		return
	}
	if h.sessions != nil {
		if token, err := c.Cookie(sessionCookieName); err == nil {
			h.deleteSession(token)
		}
	}
	clearSessionCookie(c, h.config.BasePath)
	c.Status(http.StatusNoContent)
}

func (h *authHandler) tooManyFailedAttempts(key string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.failedAttempts[key] >= maxFailedLoginAttempts
}

func (h *authHandler) recordFailedAttempt(key string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.failedAttempts[key]++
}

func (h *authHandler) clearFailedAttempts(key string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.failedAttempts, key)
}

func (h *authHandler) allowKeyOverviewRequest(token string, scopes ...string) bool {
	if h == nil || token == "" {
		return true
	}
	scope := "overview"
	if len(scopes) > 0 && strings.TrimSpace(scopes[0]) != "" {
		scope = strings.TrimSpace(scopes[0])
	}
	key := token
	if scope != "overview" {
		key = token + "\x00" + scope
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	now := time.Now()
	if last, ok := h.keyOverviewRequests[key]; ok && now.Sub(last) < time.Second {
		return false
	}
	h.keyOverviewRequests[key] = now
	return true
}

func (h *authHandler) deleteSession(token string) {
	if h == nil || token == "" {
		return
	}
	if h.sessions != nil {
		h.sessions.Delete(token)
	}
	h.clearSessionState(token)
}

func (h *authHandler) clearSessionStateForTokens(tokens []string) {
	for _, token := range tokens {
		h.clearSessionState(token)
	}
}

func (h *authHandler) clearSessionState(token string) {
	if h == nil || token == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.keyOverviewRequests, token)
	prefix := token + "\x00"
	for key := range h.keyOverviewRequests {
		if strings.HasPrefix(key, prefix) {
			delete(h.keyOverviewRequests, key)
		}
	}
}

func loginClientKey(c *gin.Context) string {
	host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	return c.ClientIP()
}

func setSessionCookie(c *gin.Context, basePath, token string, expiresAt time.Time) {
	secure := c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https"
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     sessionCookiePath(basePath),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  expiresAt,
		MaxAge:   int(time.Until(expiresAt).Seconds()),
	})
}

func sessionCookiePath(basePath string) string {
	if basePath == "" {
		return "/"
	}
	return basePath
}

func clearSessionCookie(c *gin.Context, basePath string) {
	cookiePath := sessionCookiePath(basePath)
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     cookiePath,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
	})
}
