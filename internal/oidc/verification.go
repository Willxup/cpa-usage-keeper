package oidc

import (
	"context"
	"errors"

	coreoidc "github.com/coreos/go-oidc/v3/oidc"
)

// 每次验证只保存自己的取钥结果；共享 RemoteKeySet 的缓存与并发请求合并保持不变。
type verificationKeys struct {
	keys        *coreoidc.RemoteKeySet
	unavailable bool
}

func (k *verificationKeys) VerifySignature(ctx context.Context, token string) ([]byte, error) {
	payload, err := k.keys.VerifySignature(ctx, token)
	// go-oidc 的 RemoteKeySet 只为取钥失败包装底层原因；签名不匹配直接返回独立错误。
	// 在外层 verifier 将错误格式化为文本前保留分类，避免根据原始错误字符串猜测原因。
	k.unavailable = err != nil && errors.Unwrap(err) != nil
	return payload, err
}

func (p *provider) verify(ctx context.Context, issuer, token string) (*coreoidc.IDToken, bool, error) {
	keys := &verificationKeys{keys: p.keys}
	verified, err := coreoidc.NewVerifier(issuer, keys, &p.verification).Verify(ctx, token)
	return verified, keys.unavailable || ctx.Err() != nil, err
}
