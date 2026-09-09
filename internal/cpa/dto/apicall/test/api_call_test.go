package test

import (
	"encoding/json"
	"testing"

	"cpa-usage-keeper/internal/cpa/dto/apicall"
)

func TestResponsePreservesUpstreamHeaderAndStringBody(t *testing.T) {
	var response apicall.Response
	if err := json.Unmarshal([]byte(`{"status_code":200,"header":{"Content-Type":["application/json"],"X-Request-Id":["req-1"]},"body":"{\"available_count\":1}"}`), &response); err != nil {
		t.Fatalf("unmarshal api-call response: %v", err)
	}

	if response.StatusCode != 200 || response.Header["X-Request-Id"][0] != "req-1" {
		t.Fatalf("expected upstream status and headers, got %+v", response)
	}
	var body string
	if err := json.Unmarshal(response.Body, &body); err != nil {
		t.Fatalf("decode upstream body string: %v", err)
	}
	if body != `{"available_count":1}` {
		t.Fatalf("expected exact upstream body, got %q", body)
	}
}
