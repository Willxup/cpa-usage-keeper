package oidc

import (
	"context"
	"errors"
	"net/http"
	"slices"

	"golang.org/x/oauth2"
)

func (p *provider) exchange(ctx context.Context, code, verifier string) (*oauth2.Token, error) {
	// 未声明认证方式时按标准使用 Basic；缓存选择仍须满足最新 Discovery 的能力声明。
	basic := len(p.metadata.AuthMethods) == 0 || slices.Contains(p.metadata.AuthMethods, "client_secret_basic")
	post := slices.Contains(p.metadata.AuthMethods, "client_secret_post")
	style := oauth2.AuthStyle(p.authStyle.Load())
	if !(style == oauth2.AuthStyleInHeader && basic || style == oauth2.AuthStyleInParams && post) {
		style = oauth2.AuthStyleInHeader
		if !basic {
			style = oauth2.AuthStyleInParams
		}
	}
	token, err := p.exchangeWithStyle(ctx, code, verifier, style)
	// 仅客户端认证被明确拒绝且两种方式均受支持时重试，不能覆盖 invalid_grant 或上游故障。
	var rejected *oauth2.RetrieveError
	if err != nil && ctx.Err() == nil && basic && post && errors.As(err, &rejected) && rejected.ErrorCode == "invalid_client" && rejected.Response != nil && (rejected.Response.StatusCode == http.StatusBadRequest || rejected.Response.StatusCode == http.StatusUnauthorized) {
		if style == oauth2.AuthStyleInHeader {
			style = oauth2.AuthStyleInParams
		} else {
			style = oauth2.AuthStyleInHeader
		}
		token, err = p.exchangeWithStyle(ctx, code, verifier, style)
	}
	if err == nil {
		p.authStyle.Store(int32(style))
	}
	return token, err
}

func (p *provider) exchangeWithStyle(ctx context.Context, code, verifier string, style oauth2.AuthStyle) (*oauth2.Token, error) {
	// 每次请求独立构造公开配置，避免修改共享配置或复制库内部的原子缓存。
	config := oauth2.Config{ClientID: p.oauth.ClientID, ClientSecret: p.oauth.ClientSecret, RedirectURL: p.oauth.RedirectURL, Endpoint: p.oauth.Endpoint}
	config.Endpoint.AuthStyle = style
	return config.Exchange(ctx, code, oauth2.VerifierOption(verifier))
}
