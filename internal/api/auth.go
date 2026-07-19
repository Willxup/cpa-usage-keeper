package api

import (
	"crypto/subtle"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"cpa-usage-keeper/internal/accessscope"
	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/helper"
	"cpa-usage-keeper/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	sessionCookieName      = "cpa_usage_keeper_session"
	embedSessionCookieName = "cpa_usage_keeper_embed_session"

	embedHeaderName               = "X-CPA-Usage-Keeper-Embed"
	embedHeaderValueCPAMC         = "cpamc"
	embedSessionHeaderName        = "X-CPA-Usage-Keeper-Embed-Session"
	requestIntentHeaderName       = "X-CPA-Usage-Keeper-Request"
	requestIntentHeaderValueFetch = "fetch"
)

const maxFailedLoginAttempts = 5

type AuthConfig struct {
	Enabled              bool
	LoginPassword        string
	SessionTTL           time.Duration
	BasePath             string
	FrameAncestorOrigins []string
}

type authHandler struct {
	config                      AuthConfig
	sessions                    *auth.SessionManager
	cpaAPIKeyProvider           service.CPAAPIKeyProvider
	apiKeyAuthFileScopeProvider service.APIKeyAuthFileScopeProvider

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

type loginResponse struct {
	SessionToken string `json:"session_token,omitempty"`
}

type sessionCookieKind string

const (
	sessionCookieKindStandard sessionCookieKind = "standard"
	sessionCookieKindEmbed    sessionCookieKind = "embed"
)

type sessionTokenTransport string

const (
	sessionTokenTransportCookie sessionTokenTransport = "cookie"
	sessionTokenTransportHeader sessionTokenTransport = "header"
)

type resolvedSessionToken struct {
	Token      string
	CookieKind sessionCookieKind
	Source     auth.SessionSource
	Transport  sessionTokenTransport
}

func NewAuthHandler(config AuthConfig, sessions *auth.SessionManager) *authHandler {
	return &authHandler{config: config, sessions: sessions, failedAttempts: make(map[string]int), keyOverviewRequests: make(map[string]time.Time)}
}

func (h *authHandler) setCPAAPIKeyProvider(provider service.CPAAPIKeyProvider) {
	if h != nil {
		h.cpaAPIKeyProvider = provider
	}
}

func (h *authHandler) setAPIKeyAuthFileScopeProvider(provider service.APIKeyAuthFileScopeProvider) {
	if h != nil {
		h.apiKeyAuthFileScopeProvider = provider
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

// viewerScopeMiddleware 为 API Key Viewer 解析本次请求可见的认证文件范围。
// 管理员请求不写入 scope，保持上游全部管理能力不变。
func (h *authHandler) viewerScopeMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionValue, exists := c.Get("auth_session")
		session, ok := sessionValue.(auth.Session)
		if !exists || !ok || session.Role != auth.RoleAPIKeyViewer {
			c.Next()
			return
		}
		// 先确认登录使用的 CPA API Key 仍有效。这样失效 Key 的语义保持为 401 并立即清理会话，
		// 不会被后续“未配置认证文件范围”的 403 掩盖。
		if h != nil && h.cpaAPIKeyProvider != nil {
			resolved := resolveSessionToken(c)
			if token, tokenOK := c.Get("auth_token"); tokenOK {
				if value, valueOK := token.(string); valueOK {
					resolved.Token = value
				}
			}
			if _, active := h.activeViewerAPIKey(c, resolved, session); !active {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
				return
			}
		}
		if h == nil || h.apiKeyAuthFileScopeProvider == nil || session.CPAAPIKeyID <= 0 {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "auth file access is not configured"})
			return
		}
		scope, err := h.apiKeyAuthFileScopeProvider.ResolveAPIKeyViewerScope(c.Request.Context(), session.CPAAPIKeyID)
		if err != nil {
			switch {
			case isAPIKeyAuthFileScopeAccessDenied(err):
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "auth file access is not configured"})
			default:
				writeInternalError(c, "resolve api key auth file scope failed", err)
			}
			return
		}
		c.Request = c.Request.WithContext(accessscope.WithViewerScope(c.Request.Context(), scope))
		c.Next()
	}
}

func isAPIKeyAuthFileScopeAccessDenied(err error) bool {
	return errors.Is(err, service.ErrAPIKeyAuthFileScopeNotConfigured) ||
		errors.Is(err, service.ErrAPIKeyAuthFileScopeUnresolved) ||
		errors.Is(err, gorm.ErrRecordNotFound)
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

		resolved, session, ok := h.resolveValidSession(c)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
			return
		}
		if len(allowedRoles) > 0 && !sessionRoleAllowed(session.Role, allowedRoles) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "forbidden"})
			return
		}
		c.Set("auth_token", resolved.Token)
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

func sessionMatchesResolvedSource(session auth.Session, resolved resolvedSessionToken) bool {
	return auth.NormalizeSessionSource(session.Source) == resolved.Source
}

func (h *authHandler) resolveValidSession(c *gin.Context) (resolvedSessionToken, auth.Session, bool) {
	for _, resolved := range resolveSessionTokenCandidates(c) {
		if resolved.Token == "" {
			continue
		}
		session, ok := h.sessions.Get(resolved.Token)
		if !ok {
			h.deleteSession(resolved.Token)
			if resolved.Transport == sessionTokenTransportCookie {
				clearSessionCookie(c, h.config.BasePath, resolved.CookieKind)
			}
			continue
		}
		if !sessionMatchesResolvedSource(session, resolved) {
			if resolved.Transport == sessionTokenTransportCookie {
				clearSessionCookie(c, h.config.BasePath, resolved.CookieKind)
			}
			continue
		}
		return resolved, session, true
	}
	return resolveSessionToken(c), auth.Session{}, false
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

	resolved, session, ok := h.resolveValidSession(c)
	if !ok {
		c.JSON(http.StatusOK, sessionResponse{Authenticated: false})
		return
	}
	response := sessionResponse{Authenticated: true, Role: session.Role}
	if session.Role == auth.RoleAPIKeyViewer {
		row, ok := h.activeViewerAPIKey(c, resolved, session)
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

	resolved := resolveSessionToken(c)
	token, expiresAt, err := h.sessions.CreateWithSource(resolved.Source)
	if err != nil {
		writeInternalError(c, "create auth session failed", err)
		return
	}

	setSessionCookie(c, h.config.BasePath, resolved.CookieKind, token, expiresAt)
	writeLoginSuccess(c, resolved, token)
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
	if h.apiKeyAuthFileScopeProvider != nil {
		if _, err := h.apiKeyAuthFileScopeProvider.ResolveAPIKeyViewerScope(c.Request.Context(), row.ID); err != nil {
			if isAPIKeyAuthFileScopeAccessDenied(err) {
				c.JSON(http.StatusForbidden, gin.H{"error": "auth file access is not configured"})
				return
			}
			writeInternalError(c, "resolve api key auth file scope during login failed", err)
			return
		}
	}
	resolved := resolveSessionToken(c)
	token, expiresAt, err := h.sessions.CreateAPIKeyViewerWithSource(row.ID, resolved.Source)
	if err != nil {
		writeInternalError(c, "create api key viewer session failed", err)
		return
	}
	setSessionCookie(c, h.config.BasePath, resolved.CookieKind, token, expiresAt)
	writeLoginSuccess(c, resolved, token)
}

func (h *authHandler) activeViewerAPIKey(c *gin.Context, resolved resolvedSessionToken, session auth.Session) (entities.CPAAPIKey, bool) {
	if h.cpaAPIKeyProvider == nil || session.CPAAPIKeyID <= 0 {
		h.deleteSession(resolved.Token)
		clearSessionCookie(c, h.config.BasePath, resolved.CookieKind)
		return entities.CPAAPIKey{}, false
	}
	row, err := h.cpaAPIKeyProvider.FindActiveCPAAPIKeyByID(c.Request.Context(), session.CPAAPIKeyID)
	if err != nil {
		h.deleteSession(resolved.Token)
		clearSessionCookie(c, h.config.BasePath, resolved.CookieKind)
		return entities.CPAAPIKey{}, false
	}
	return row, true
}

func (h *authHandler) logout(c *gin.Context) {
	if h == nil || !h.config.Enabled {
		c.Status(http.StatusNoContent)
		return
	}
	resolved, _, ok := h.resolveValidSession(c)
	if !ok {
		resolved = resolveSessionToken(c)
	}
	if h.sessions != nil {
		h.deleteSession(resolved.Token)
	}
	clearSessionCookie(c, h.config.BasePath, resolved.CookieKind)
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

func isCPAMCEmbedRequest(c *gin.Context) bool {
	return strings.EqualFold(strings.TrimSpace(c.GetHeader(embedHeaderName)), embedHeaderValueCPAMC)
}

func resolveSessionToken(c *gin.Context) resolvedSessionToken {
	if candidates := resolveSessionTokenCandidates(c); len(candidates) > 0 {
		return candidates[0]
	}
	if isCPAMCEmbedRequest(c) {
		return resolvedSessionToken{CookieKind: sessionCookieKindEmbed, Source: auth.SessionSourceEmbed, Transport: sessionTokenTransportCookie}
	}
	return resolvedSessionToken{CookieKind: sessionCookieKindStandard, Source: auth.SessionSourceStandard, Transport: sessionTokenTransportCookie}
}

func resolveSessionTokenCandidates(c *gin.Context) []resolvedSessionToken {
	if isCPAMCEmbedRequest(c) {
		var candidates []resolvedSessionToken
		cookieToken, _ := c.Cookie(embedSessionCookieName)
		if cookieToken != "" {
			candidates = append(candidates, resolvedSessionToken{Token: cookieToken, CookieKind: sessionCookieKindEmbed, Source: auth.SessionSourceEmbed, Transport: sessionTokenTransportCookie})
		}
		headerToken := strings.TrimSpace(c.GetHeader(embedSessionHeaderName))
		if headerToken != "" && headerToken != cookieToken {
			candidates = append(candidates, resolvedSessionToken{Token: headerToken, CookieKind: sessionCookieKindEmbed, Source: auth.SessionSourceEmbed, Transport: sessionTokenTransportHeader})
		}
		return candidates
	}
	token, _ := c.Cookie(sessionCookieName)
	if token == "" {
		return nil
	}
	return []resolvedSessionToken{{Token: token, CookieKind: sessionCookieKindStandard, Source: auth.SessionSourceStandard, Transport: sessionTokenTransportCookie}}
}

func writeLoginSuccess(c *gin.Context, resolved resolvedSessionToken, token string) {
	if resolved.Source == auth.SessionSourceEmbed {
		c.JSON(http.StatusOK, loginResponse{SessionToken: token})
		return
	}
	c.Status(http.StatusNoContent)
}

func requiresRequestIntent(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func requestIntentMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if requiresRequestIntent(c.Request.Method) && c.GetHeader(requestIntentHeaderName) != requestIntentHeaderValueFetch {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "fetch request required"})
			return
		}
		c.Next()
	}
}

func setSessionCookie(c *gin.Context, basePath string, kind sessionCookieKind, token string, expiresAt time.Time) {
	cookie := sessionCookie(basePath, kind)
	if kind == sessionCookieKindStandard {
		cookie.Secure = c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https"
	}
	cookie.Value = token
	cookie.Expires = expiresAt
	cookie.MaxAge = int(time.Until(expiresAt).Seconds())
	http.SetCookie(c.Writer, cookie)
}

func sessionCookiePath(basePath string) string {
	if basePath == "" {
		return "/"
	}
	return basePath
}

func clearSessionCookie(c *gin.Context, basePath string, kind sessionCookieKind) {
	cookie := sessionCookie(basePath, kind)
	if kind == sessionCookieKindStandard {
		cookie.Secure = c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https"
	}
	cookie.Value = ""
	cookie.Expires = time.Unix(0, 0)
	cookie.MaxAge = -1
	http.SetCookie(c.Writer, cookie)
}

func sessionCookie(basePath string, kind sessionCookieKind) *http.Cookie {
	cookie := &http.Cookie{
		Name:     sessionCookieName,
		Path:     sessionCookiePath(basePath),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
	if kind == sessionCookieKindEmbed {
		cookie.Name = embedSessionCookieName
		cookie.Secure = true
		cookie.SameSite = http.SameSiteNoneMode
		cookie.Partitioned = true
		return cookie
	}
	return cookie
}
