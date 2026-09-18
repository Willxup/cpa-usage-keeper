package test

import (
	"testing"
	"time"

	"cpa-usage-keeper/internal/service"
)

func TestDecodeRedisUsageMessageKeepsUpstreamResponseModel(t *testing.T) {
	event, _, err := service.DecodeRedisUsageMessage(`{
		"request_id":"req-up",
		"model":"mapped-model",
		"upstream_response_model":"gpt-5.4",
		"tokens":{}
	}`, time.Date(2026, 9, 17, 1, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if event.UpstreamResponseModel != "gpt-5.4" {
		t.Fatalf("got %q", event.UpstreamResponseModel)
	}
}

func TestDecodeRedisUsageMessageLeavesMissingUpstreamResponseModelEmpty(t *testing.T) {
	event, _, err := service.DecodeRedisUsageMessage(`{
		"request_id":"req-up-missing","model":"mapped-model","tokens":{}
	}`, time.Date(2026, 9, 17, 1, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if event.UpstreamResponseModel != "" {
		t.Fatalf("got %q", event.UpstreamResponseModel)
	}
}
