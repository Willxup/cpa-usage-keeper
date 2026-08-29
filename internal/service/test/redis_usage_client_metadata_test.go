package test

import (
	"testing"
	"time"

	"cpa-usage-keeper/internal/service"
)

func TestDecodeRedisUsageMessageKeepsClientMetadata(t *testing.T) {
	event, _, err := service.DecodeRedisUsageMessage(`{
		"request_id":"req-client-metadata",
		"client_ip":"192.0.2.10",
		"x_forwarded_for":"203.0.113.5, 198.51.100.8",
		"user_agent":"test-client/1.0",
		"tokens":{}
	}`, time.Date(2026, 7, 29, 1, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("DecodeRedisUsageMessage returned error: %v", err)
	}
	if event.ClientIP == nil || *event.ClientIP != "192.0.2.10" {
		t.Fatalf("expected client_ip to be preserved, got %#v", event.ClientIP)
	}
	if event.XForwardedFor == nil || *event.XForwardedFor != "203.0.113.5, 198.51.100.8" {
		t.Fatalf("expected x_forwarded_for to be preserved, got %#v", event.XForwardedFor)
	}
	if event.UserAgent == nil || *event.UserAgent != "test-client/1.0" {
		t.Fatalf("expected user_agent to be preserved, got %#v", event.UserAgent)
	}
}

func TestDecodeRedisUsageMessageLeavesMissingClientMetadataNull(t *testing.T) {
	event, _, err := service.DecodeRedisUsageMessage(`{
		"request_id":"req-client-metadata-missing",
		"tokens":{}
	}`, time.Date(2026, 7, 29, 1, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("DecodeRedisUsageMessage returned error: %v", err)
	}
	if event.ClientIP != nil || event.XForwardedFor != nil || event.UserAgent != nil {
		t.Fatalf("expected missing client metadata to stay nil, got client_ip=%#v x_forwarded_for=%#v user_agent=%#v", event.ClientIP, event.XForwardedFor, event.UserAgent)
	}
}
