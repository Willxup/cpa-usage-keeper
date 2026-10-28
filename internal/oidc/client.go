package oidc

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"golang.org/x/oauth2"
)

const ClockSkew = time.Minute

type Client struct {
	config       Config
	http         *http.Client
	transactions *TransactionStore
	mu           sync.Mutex
	cached       *provider
	flight       *discoveryFlight
	retryAt      time.Time
	httpWarning  sync.Once
}

type Authorization struct {
	URL       string
	State     string
	Binding   string
	ExpiresAt time.Time
}

type Identity struct {
	Issuer, Subject, ClientID      string
	Name, PreferredUsername, Email string
}

type LoginResult struct {
	Identity  Identity
	ReturnTo  string
	SourceKey string
	// AuthenticationFailed 只标记已绑定事务中确认的认证拒绝，取消和外部故障不计入。
	AuthenticationFailed bool
}

// NewClient 不访问 IdP，外部暂时故障不能阻塞 Keeper 启动和密码登录。
// httpClient 允许调用方注入受信任的内部 CA；不会继承 CPA 的跳过 TLS 校验选项。
func NewClient(config Config, httpClient *http.Client) (*Client, error) {
	if _, err := ParseEndpoint(config.Issuer); err != nil {
		return nil, ErrInvalid
	}
	if _, err := ParseEndpoint(config.RedirectURL); err != nil {
		return nil, ErrInvalid
	}
	if config.ClientID == "" || config.ClientSecret == "" {
		return nil, ErrInvalid
	}
	client := &http.Client{Timeout: ExternalRequestTimeout}
	if httpClient != nil {
		*client = *httpClient
	}
	if client.Timeout <= 0 || client.Timeout > ExternalRequestTimeout {
		client.Timeout = ExternalRequestTimeout
	}
	// 凭据请求不得随重定向流向另一个端点；Discovery 也使用相同的可预测规则。
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	config.AllowedSubjects = append([]string(nil), config.AllowedSubjects...)
	return &Client{config: config, http: client, transactions: NewTransactionStore(TransactionOptions{})}, nil
}

func (c *Client) Config() Config {
	result := c.config
	result.ClientSecret = ""
	result.AllowedSubjects = append([]string(nil), result.AllowedSubjects...)
	return result
}

func (c *Client) Begin(ctx context.Context, returnTo string, bindings map[string]string, sourceKey string) (Authorization, error) {
	p, err := c.getProvider(ctx)
	if err != nil {
		return Authorization{}, err
	}
	txn, binding, err := c.transactions.Create(c.config.Issuer, c.config.ClientID, returnTo, sourceKey, bindings)
	if err != nil {
		return Authorization{}, err
	}
	authorizationURL, err := url.Parse(p.oauth.AuthCodeURL(txn.State, oauth2.SetAuthURLParam("nonce", txn.Nonce), oauth2.SetAuthURLParam("response_mode", "query"), oauth2.S256ChallengeOption(txn.Verifier)))
	if err != nil {
		logFailure("authorization", "invalid_endpoint")
		return Authorization{}, ErrUnavailable
	}
	// prompt 仅由本地开关决定，避免元数据端点已有的同名参数覆盖此策略。
	query := authorizationURL.Query()
	query.Del("prompt")
	if c.config.PromptLogin {
		query.Set("prompt", "login")
	}
	authorizationURL.RawQuery = query.Encode()
	return Authorization{URL: authorizationURL.String(), State: txn.State, Binding: binding, ExpiresAt: txn.ExpiresAt}, nil
}

// Complete 先消费已绑定事务，再访问端点；同一回调无论成功或失败都只能执行一次。
func (c *Client) Complete(ctx context.Context, state, binding, code, providerError string) (LoginResult, error) {
	txn, err := c.transactions.Consume(state, binding)
	if err != nil {
		return LoginResult{}, err
	}
	result := LoginResult{ReturnTo: txn.ReturnTo, SourceKey: txn.SourceKey}
	if providerError != "" {
		if providerError == "access_denied" {
			return result, ErrCanceled
		}
		logrus.WithFields(logrus.Fields{"stage": "authorization", "oauth_error": safeOAuthError(providerError)}).Warn("OIDC authorization failed")
		result.AuthenticationFailed = providerError == "invalid_response"
		return result, ErrInvalid
	}
	if code == "" || txn.Issuer != c.config.Issuer || txn.ClientID != c.config.ClientID {
		result.AuthenticationFailed = true
		logFailure("callback", "invalid_authorization_response")
		return result, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, CallbackTimeout)
	defer cancel()
	p, err := c.getProvider(ctx)
	if err != nil {
		return result, err
	}
	token, err := p.exchange(context.WithValue(ctx, oauth2.HTTPClient, c.http), code, txn.Verifier)
	if err != nil {
		// 依赖可能把正文读取错误转成文本，直接检查请求取消状态，避免误记上游故障。
		if !errors.Is(ctx.Err(), context.Canceled) {
			logRequestFailure("token", 0, err)
		}
		var responseError *oauth2.RetrieveError
		if errors.As(err, &responseError) && responseError.ErrorCode == "invalid_grant" && responseError.Response != nil && responseError.Response.StatusCode < 500 && responseError.Response.StatusCode != http.StatusTooManyRequests {
			result.AuthenticationFailed = true
		}
		return result, ErrUnavailable
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		result.AuthenticationFailed = true
		logFailure("id_token", "missing_id_token")
		return result, ErrInvalid
	}
	verified, keysUnavailable, err := p.verify(ctx, c.config.Issuer, rawIDToken)
	if err != nil {
		result.AuthenticationFailed = !keysUnavailable
		// 浏览器中断等待不记故障；超时、取钥故障和真实验签失败仍保留一条诊断。
		if !errors.Is(ctx.Err(), context.Canceled) {
			logFailure("id_token", "verification_failed")
		}
		return result, ErrInvalid
	}
	var claims map[string]json.RawMessage
	if verified.Claims(&claims) != nil || !validClaims(claims, c.config.Issuer, c.config.ClientID, txn.Nonce, time.Now()) {
		result.AuthenticationFailed = true
		logFailure("id_token", "invalid_claims")
		return result, ErrInvalid
	}
	if verified.Issuer != c.config.Issuer || subtle.ConstantTimeCompare([]byte(verified.Nonce), []byte(txn.Nonce)) != 1 {
		result.AuthenticationFailed = true
		logFailure("id_token", "issuer_or_nonce_mismatch")
		return result, ErrInvalid
	}
	if verified.AccessTokenHash != "" && (token.AccessToken == "" || verified.VerifyAccessToken(token.AccessToken) != nil) {
		result.AuthenticationFailed = true
		logFailure("id_token", "access_token_hash_mismatch")
		return result, ErrInvalid
	}
	identity := Identity{Issuer: verified.Issuer, Subject: verified.Subject, ClientID: c.config.ClientID, Name: optionalString(claims, "name"), PreferredUsername: optionalString(claims, "preferred_username"), Email: optionalString(claims, "email")}
	if !c.config.AllowsSubject(identity.Subject) {
		result.AuthenticationFailed = true
		logFailure("admission", "subject_not_allowed")
		return result, ErrDenied
	}
	if identity.Name == "" && p.metadata.UserInfoEndpoint != "" && token.AccessToken != "" {
		userinfo, err := c.userInfo(ctx, p.metadata.UserInfoEndpoint, token.AccessToken)
		if err == nil {
			if optionalString(userinfo, "sub") != identity.Subject {
				result.AuthenticationFailed = true
				logFailure("userinfo", "subject_mismatch")
				return result, ErrInvalid
			}
			identity.Name = optionalString(userinfo, "name")
			if identity.PreferredUsername == "" {
				identity.PreferredUsername = optionalString(userinfo, "preferred_username")
			}
			if identity.Email == "" {
				identity.Email = optionalString(userinfo, "email")
			}
		}
	}
	result.Identity = identity
	return result, nil
}

func (c *Client) userInfo(ctx context.Context, endpoint, accessToken string) (map[string]json.RawMessage, error) {
	// 资料补全失败沿用 ID Token 中的身份；只有 subject 不匹配导致拒绝登录时才记诊断。
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, ErrUnavailable
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	response, err := c.http.Do(req)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, ErrUnavailable
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil || len(body) > maxResponseBytes {
		return nil, ErrUnavailable
	}
	var claims map[string]json.RawMessage
	if json.Unmarshal(body, &claims) != nil || claims == nil {
		return nil, ErrUnavailable
	}
	return claims, nil
}
