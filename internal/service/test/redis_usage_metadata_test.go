package test

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"cpa-usage-keeper/internal/service"
)

func TestDecodeRedisUsageMessagePrefersResolvedClientIP(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		resolved, peer, want *string
	}{
		{"ipv4", new("203.0.113.5"), new("172.19.0.7"), new("203.0.113.5")},
		{"ipv6", new("2001:db8::5"), new("172.19.0.7"), new("2001:db8::5")},
		{"whitespace", new(" 203.0.113.5 "), new("172.19.0.7"), new("203.0.113.5")},
		{"empty", new(""), new("172.19.0.7"), new("172.19.0.7")},
		{"null", nil, new("172.19.0.7"), new("172.19.0.7")},
		{"invalid", new("not-an-ip"), new("172.19.0.7"), new("172.19.0.7")},
		{"forwarded-list", new("203.0.113.5, 172.19.0.1"), new("172.19.0.7"), new("172.19.0.7")},
		{"resolved-without-peer", new("203.0.113.5"), nil, new("203.0.113.5")},
		{"invalid-without-peer", new("not-an-ip"), nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload, err := json.Marshal(map[string]any{
				"request_id": "resolved-client", "client_ip": tc.peer,
				"resolved_client_ip": tc.resolved, "x_forwarded_for": "203.0.113.5, 172.19.0.1",
				"user_agent": "test-client/1.0",
			})
			if err != nil {
				t.Fatal(err)
			}
			event, raw, err := service.DecodeRedisUsageMessage(string(payload), time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(event.ClientIP, tc.want) {
				t.Fatalf("client_ip = %v, want %v", event.ClientIP, tc.want)
			}
			if event.XForwardedFor == nil || *event.XForwardedFor != "203.0.113.5, 172.19.0.1" {
				t.Fatal("forwarding evidence changed")
			}
			if event.UserAgent == nil || *event.UserAgent != "test-client/1.0" {
				t.Fatal("user agent changed")
			}
			if string(raw) != string(payload) {
				t.Fatal("original usage payload changed")
			}
		})
	}
}

func TestDecodeRedisUsageMessagePreservesOptionalMetadata(t *testing.T) {
	type metadata struct {
		client        [3]*string
		requestModel  string
		responseModel string
		requestTier   string
		responseTier  string
	}
	for _, tc := range []struct {
		name    string
		message string
		want    metadata
	}{
		{
			name:    "populated",
			message: `{"request_id":"req-metadata","model":"gpt-6-astra","response_model":"gpt-5.6-luna","client_ip":"192.0.2.10","x_forwarded_for":"203.0.113.5, 198.51.100.8","user_agent":"test-client/1.0","service_tier":"auto","response_service_tier":"default","tokens":{}}`,
			want:    metadata{client: [3]*string{new("192.0.2.10"), new("203.0.113.5, 198.51.100.8"), new("test-client/1.0")}, requestModel: "gpt-6-astra", responseModel: "gpt-5.6-luna", requestTier: "auto", responseTier: "default"},
		},
		{
			name:    "missing",
			message: `{"request_id":"req-metadata-missing","model":"gpt-5.6-luna","service_tier":"auto","tokens":{}}`,
			want:    metadata{requestModel: "gpt-5.6-luna", requestTier: "auto"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			event, _, err := service.DecodeRedisUsageMessage(tc.message, time.Date(2026, 7, 29, 1, 0, 0, 0, time.UTC))
			if err != nil {
				t.Fatalf("DecodeRedisUsageMessage: %v", err)
			}
			got := metadata{
				client:        [3]*string{event.ClientIP, event.XForwardedFor, event.UserAgent},
				requestModel:  event.Model,
				responseModel: event.ResponseModel,
				requestTier:   event.ServiceTier,
				responseTier:  event.ResponseServiceTier,
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("metadata = %+v, want %+v", got, tc.want)
			}
		})
	}
}
