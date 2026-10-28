package api

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"path"
	"strings"
	"time"
	"unicode"

	"cpa-usage-keeper/internal/oidc"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

func (h *authHandler) oidcLogin(c *gin.Context) {
	setNoStoreHeaders(c)
	if h == nil || !h.config.Enabled || h.config.OIDC == nil || h.sessions == nil || isOIDCEmbedRequest(c) {
		c.JSON(http.StatusForbidden, gin.H{"code": "oidc_failed", "error": "OIDC login is unavailable"})
		return
	}
	if !h.oidcPublicOriginMatches(c) {
		c.JSON(http.StatusBadRequest, gin.H{"code": "oidc_public_origin", "error": "Use the configured Keeper public address to sign in"})
		return
	}
	allowed, retryAfter := h.loginAttempts.AllowPending("oidc:" + loginClientKey(c))
	if !writeLoginAttemptResult(c, allowed, retryAfter) {
		return
	}
	var request struct {
		ReturnTo string `json:"return_to"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		if isRequestEntityTooLarge(err) {
			writeRequestEntityTooLarge(c)
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"code": "oidc_failed", "error": "invalid login request"})
		return
	}
	returnTo, ok := oidcReturnTo(request.ReturnTo, h.config.BasePath)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"code": "oidc_failed", "error": "invalid return target"})
		return
	}
	bindings := make(map[string]string)
	for _, cookie := range c.Request.Cookies() {
		if strings.HasPrefix(cookie.Name, oidc.BindingCookiePrefix) {
			bindings[strings.TrimPrefix(cookie.Name, oidc.BindingCookiePrefix)] = cookie.Value
		}
	}
	authorization, err := h.config.OIDC.Begin(c.Request.Context(), returnTo, bindings, "oidc:"+loginClientKey(c))
	if err != nil {
		status := http.StatusServiceUnavailable
		if errors.Is(err, oidc.ErrCapacity) {
			status = http.StatusTooManyRequests
			c.Header("Retry-After", "60")
		}
		c.JSON(status, gin.H{"code": oidcErrorCode(err), "error": "Unable to start OIDC login"})
		return
	}
	h.setOIDCBindingCookie(c, authorization.State, authorization.Binding, authorization.ExpiresAt)
	c.JSON(http.StatusOK, gin.H{"authorization_url": authorization.URL})
}

func (h *authHandler) oidcCallback(c *gin.Context) {
	setNoStoreHeaders(c)
	c.Header("Referrer-Policy", "no-referrer")
	if h == nil {
		c.Status(http.StatusForbidden)
		return
	}
	fail := func(target string, err error) {
		if target == "" {
			target = h.config.BasePath + "/"
		}
		parsed, _ := url.Parse(target)
		query := parsed.Query()
		query.Set("oidc_error", oidcErrorCode(err))
		parsed.RawQuery = query.Encode()
		c.Redirect(http.StatusSeeOther, parsed.String())
	}
	if !h.config.Enabled || h.config.OIDC == nil || h.sessions == nil || isOIDCEmbedRequest(c) || !h.oidcPublicOriginMatches(c) {
		fail("", oidc.ErrInvalid)
		return
	}
	query := c.Request.URL.Query()
	state := query.Get("state")
	if len(query["state"]) != 1 || !validOIDCState(state) {
		fail("", oidc.ErrExpired)
		return
	}
	binding, _ := c.Cookie(oidc.BindingCookiePrefix + state)
	code, providerError := query.Get("code"), query.Get("error")
	if len(query["code"]) > 1 || len(query["error"]) > 1 || code != "" && providerError != "" {
		code = ""
		providerError = "invalid_response"
	}
	result, err := h.config.OIDC.Complete(c.Request.Context(), state, binding, code, providerError)
	h.setOIDCBindingCookie(c, state, "", time.Unix(0, 0))
	if err != nil {
		if result.AuthenticationFailed {
			h.loginAttempts.RecordFailure(result.SourceKey)
		}
		fail(result.ReturnTo, err)
		return
	}
	identity := result.Identity
	token, expiresAt, err := h.sessions.CreateOIDC(identity.Issuer, identity.Subject, identity.ClientID, oidcDisplayAlias(identity), sessionClientMetadata(c))
	if err != nil {
		logrus.WithField("stage", "session").Warn("Unable to create OIDC session")
		fail(result.ReturnTo, oidc.ErrInvalid)
		return
	}
	// 回调可能换出口 IP，成功后只重置最初发起者的失败计数。
	h.loginAttempts.Reset(result.SourceKey)
	// 回调公开协议是已校验的静态配置，不因内部 HTTP 或 IdP 的协议改变 Cookie。
	cookie := sessionCookie(h.config.BasePath, sessionCookieKindStandard)
	callbackURL, _ := url.Parse(h.config.OIDC.Config().RedirectURL)
	cookie.Secure = callbackURL.Scheme == "https"
	cookie.Value, cookie.Expires, cookie.MaxAge = token, expiresAt, int(time.Until(expiresAt).Seconds())
	http.SetCookie(c.Writer, cookie)
	c.Redirect(http.StatusSeeOther, result.ReturnTo)
}

func (h *authHandler) setOIDCBindingCookie(c *gin.Context, state, binding string, expires time.Time) {
	callback, _ := url.Parse(h.config.OIDC.Config().RedirectURL)
	maxAge := int(time.Until(expires).Seconds())
	if binding == "" {
		maxAge = -1
	}
	// 起始与回调共享最窄路径，起始请求才能携带既有事务并执行单浏览器上限。
	http.SetCookie(c.Writer, &http.Cookie{Name: oidc.BindingCookiePrefix + state, Value: binding, Path: strings.TrimSuffix(callback.Path, "/callback"), HttpOnly: true, Secure: callback.Scheme == "https", SameSite: http.SameSiteLaxMode, Expires: expires, MaxAge: maxAge})
}

func isOIDCEmbedRequest(c *gin.Context) bool {
	if isCPAMCEmbedRequest(c) || c.GetHeader("Sec-Fetch-Dest") == "iframe" || c.GetHeader("Sec-Fetch-Dest") == "frame" {
		return true
	}
	for _, key := range []string{"embed", "mode"} {
		for _, value := range c.QueryArray(key) {
			if value == "cpamc" {
				return true
			}
		}
	}
	return false
}

func (h *authHandler) oidcPublicOriginMatches(c *gin.Context) bool {
	expected, _ := url.Parse(h.config.OIDC.Config().RedirectURL)
	scheme, host := "http", c.Request.Host
	if c.Request.TLS != nil {
		scheme = "https"
	}
	trustedProxy := h.isTrustedOIDCProxy(c.Request.RemoteAddr)
	if trustedProxy {
		if forwarded := c.GetHeader("X-Forwarded-Proto"); forwarded == "http" || forwarded == "https" {
			scheme = forwarded
		}
		if forwarded := c.GetHeader("X-Forwarded-Host"); forwarded != "" && !strings.ContainsAny(forwarded, ",/\\@ \t\r\n") {
			host = forwarded
		}
	}
	actual, err := url.Parse(scheme + "://" + host)
	// 仅记录用于比较的 origin；不输出回调查询、Cookie 或未经筛选的代理头。
	logMismatch := func(reason string, browserOrigin *url.URL) {
		fields := logrus.Fields{
			"stage": "public_origin", "reason": reason,
			"expected_origin": oidcDiagnosticOrigin(expected), "actual_origin": oidcDiagnosticOrigin(actual),
			"trusted_proxy": trustedProxy,
		}
		if c.GetHeader("Origin") != "" {
			fields["browser_origin"] = oidcDiagnosticOrigin(browserOrigin)
		}
		logrus.WithFields(fields).Warn("OIDC public origin mismatch")
	}
	if err != nil || publicOIDCOrigin(actual) != publicOIDCOrigin(expected) {
		browserOrigin, _ := url.Parse(c.GetHeader("Origin"))
		logMismatch("request_origin_mismatch", browserOrigin)
		return false
	}
	if origin := c.GetHeader("Origin"); origin != "" && origin != publicOIDCOrigin(expected) {
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || publicOIDCOrigin(parsed) != publicOIDCOrigin(expected) {
			logMismatch("browser_origin_mismatch", parsed)
			return false
		}
	}
	return true
}

func oidcDiagnosticOrigin(value *url.URL) string {
	if value == nil || value.Hostname() == "" || (value.Scheme != "http" && value.Scheme != "https") {
		return "invalid"
	}
	origin := publicOIDCOrigin(value)
	if len(origin) > 300 || strings.IndexFunc(origin, unicode.IsControl) >= 0 {
		return "invalid"
	}
	return origin
}

func (h *authHandler) isTrustedOIDCProxy(remote string) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, cidr := range append(append([]string{}, loopbackTrustedProxyCIDRs...), h.config.TrustedProxyCIDRs...) {
		_, network, err := net.ParseCIDR(cidr)
		if err == nil && network.Contains(ip) {
			return true
		}
	}
	return false
}

func publicOIDCOrigin(u *url.URL) string {
	port := u.Port()
	if u.Scheme == "http" && port == "80" || u.Scheme == "https" && port == "443" {
		port = ""
	}
	host := strings.ToLower(u.Hostname())
	if strings.Contains(host, ":") {
		// 仅比较 IPv6 的等价写法；不 Unmap 到 IPv4，也不改写注册的 redirect_uri 或 issuer。
		if address, err := netip.ParseAddr(host); err == nil && address.Is6() {
			host = address.String()
		}
		host = "[" + host + "]"
	}
	if port != "" {
		host += ":" + port
	}
	return strings.ToLower(u.Scheme) + "://" + host
}

func oidcReturnTo(value, basePath string) (string, bool) {
	if value == "" {
		return basePath + "/", true
	}
	u, err := url.Parse(value)
	if err != nil || u.IsAbs() || u.Host != "" || u.Opaque != "" || !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") || u.RawPath != "" || (path.Clean(u.Path) != u.Path && path.Clean(u.Path)+"/" != u.Path) {
		return "", false
	}
	if basePath != "" && u.Path != basePath && !strings.HasPrefix(u.Path, basePath+"/") {
		return "", false
	}
	decoded, err := url.QueryUnescape(value)
	if err != nil || strings.ContainsAny(decoded, "\\\r\n") || strings.IndexFunc(decoded, unicode.IsControl) >= 0 {
		return "", false
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return "", false
	}
	for _, key := range []string{"embed", "mode"} {
		for _, item := range query[key] {
			if item == "cpamc" {
				return "", false
			}
		}
	}
	if strings.HasPrefix(strings.TrimPrefix(u.Path, basePath), "/api/") {
		return "", false
	}
	return value, true
}

func validOIDCState(state string) bool {
	if len(state) != 43 {
		return false
	}
	for _, r := range state {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

func oidcErrorCode(err error) string {
	switch {
	case errors.Is(err, oidc.ErrCanceled):
		return "oidc_canceled"
	case errors.Is(err, oidc.ErrExpired):
		return "oidc_expired"
	case errors.Is(err, oidc.ErrDenied):
		return "oidc_denied"
	case errors.Is(err, oidc.ErrUnavailable):
		return "oidc_unavailable"
	case errors.Is(err, oidc.ErrCapacity):
		return "login_rate_limited"
	default:
		return "oidc_failed"
	}
}

func oidcDisplayAlias(identity oidc.Identity) string {
	for _, value := range []string{identity.Name, identity.PreferredUsername, identity.Email, identity.Subject} {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		runes := []rune(value)
		if len(runes) > maxUsageIdentityAliasLength {
			runes = runes[:maxUsageIdentityAliasLength]
		}
		value = string(runes)
		if validateUsageIdentityAlias(value) == nil {
			return value
		}
	}
	return ""
}
