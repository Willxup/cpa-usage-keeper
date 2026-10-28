package oidc

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// Config 只描述单个静态注册的服务器端客户端，不接受浏览器传入协议端点。
type Config struct {
	Enabled         bool
	PromptLogin     bool
	Issuer          string
	ClientID        string
	ClientSecret    string
	RedirectURL     string
	AllowedSubjects []string
}

func ParseAllowedSubjects(value string) ([]string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	var subjects []string
	if !strings.HasPrefix(strings.TrimSpace(value), "[") || json.Unmarshal([]byte(value), &subjects) != nil {
		return nil, fmt.Errorf("AUTH_OIDC_ALLOWED_SUBJECTS must be a JSON string array")
	}
	seen := make(map[string]bool, len(subjects))
	for _, subject := range subjects {
		if subject == "" || seen[subject] {
			return nil, fmt.Errorf("AUTH_OIDC_ALLOWED_SUBJECTS must contain non-empty, unique subjects")
		}
		seen[subject] = true
	}
	return subjects, nil
}

func (c Config) Validate(basePath string, authEnabled bool) error {
	if !c.Enabled {
		return nil
	}
	if !authEnabled {
		return fmt.Errorf("AUTH_OIDC_ENABLED requires AUTH_ENABLED=true and LOGIN_PASSWORD")
	}
	for _, field := range []struct{ name, value string }{
		{"AUTH_OIDC_ISSUER", c.Issuer}, {"AUTH_OIDC_CLIENT_ID", c.ClientID},
		{"AUTH_OIDC_CLIENT_SECRET", c.ClientSecret}, {"AUTH_OIDC_REDIRECT_URL", c.RedirectURL},
	} {
		if field.value == "" {
			return fmt.Errorf("%s is required when AUTH_OIDC_ENABLED is true", field.name)
		}
	}
	if _, err := ParseEndpoint(c.Issuer); err != nil {
		return fmt.Errorf("AUTH_OIDC_ISSUER must be an absolute HTTP/HTTPS URL without credentials, query or fragment")
	}
	redirect, err := ParseEndpoint(c.RedirectURL)
	if err != nil || redirect.Path != basePath+"/api/v1/auth/oidc/callback" || redirect.RawPath != "" {
		return fmt.Errorf("AUTH_OIDC_REDIRECT_URL must be an absolute HTTP/HTTPS URL with the exact path %s/api/v1/auth/oidc/callback and no credentials, query or fragment", basePath)
	}
	return nil
}

// ParseEndpoint 校验本地身份 URL；HTTP 只由显式 URL 选择，查询仅对元数据端点开放。
func ParseEndpoint(value string) (*url.URL, error) {
	return parseEndpoint(value, false)
}

func parseEndpoint(value string, allowQuery bool) (*url.URL, error) {
	u, err := url.Parse(value)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || (!allowQuery && (u.RawQuery != "" || u.ForceQuery)) || strings.Contains(value, "#") || u.Opaque != "" {
		return nil, fmt.Errorf("invalid OIDC endpoint")
	}
	return u, nil
}

func (c Config) AllowsSubject(subject string) bool {
	if subject == "" {
		return false
	}
	if len(c.AllowedSubjects) == 0 {
		return true
	}
	for _, allowed := range c.AllowedSubjects {
		if subject == allowed {
			return true
		}
	}
	return false
}
