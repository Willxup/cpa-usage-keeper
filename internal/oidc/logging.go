package oidc

import (
	"context"
	"crypto/tls"
	"errors"
	"net"

	"github.com/sirupsen/logrus"
	"golang.org/x/oauth2"
)

func logFailure(stage, reason string) {
	logrus.WithFields(logrus.Fields{"stage": stage, "reason": reason}).Warn("OIDC operation failed")
}

// 外部错误可能包含端点查询、授权码或完整响应；只提取可控分类和标准错误码。
func logRequestFailure(stage string, status int, err error) {
	// 浏览器关闭或主动中止请求不属于需要排错的上游故障。
	if errors.Is(err, context.Canceled) {
		return
	}
	fields := logrus.Fields{"stage": stage, "reason": "request_failed"}
	var responseError *oauth2.RetrieveError
	if errors.As(err, &responseError) {
		if responseError.Response != nil {
			status = responseError.Response.StatusCode
		}
		fields["reason"] = "token_response_rejected"
		if responseError.ErrorCode != "" {
			fields["oauth_error"] = safeOAuthError(responseError.ErrorCode)
		}
	} else if err != nil {
		var certificateError *tls.CertificateVerificationError
		var dnsError *net.DNSError
		var networkError net.Error
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			fields["reason"] = "timeout"
		case errors.As(err, &certificateError):
			fields["reason"] = "tls_certificate_invalid"
		case errors.As(err, &dnsError):
			fields["reason"] = "dns_failed"
		case errors.As(err, &networkError) && networkError.Timeout():
			fields["reason"] = "timeout"
		}
	} else if status != 0 {
		fields["reason"] = "unexpected_http_status"
	}
	if status != 0 {
		fields["http_status"] = status
	}
	logrus.WithFields(fields).Warn("OIDC operation failed")
}

func safeOAuthError(code string) string {
	switch code {
	case "invalid_request", "unauthorized_client", "access_denied", "unsupported_response_type",
		"invalid_scope", "server_error", "temporarily_unavailable", "invalid_client", "invalid_grant",
		"unsupported_grant_type", "interaction_required", "login_required", "account_selection_required",
		"consent_required", "invalid_request_uri", "invalid_request_object", "request_not_supported",
		"request_uri_not_supported", "registration_not_supported", "invalid_response":
		return code
	default:
		return "unrecognized_error"
	}
}
