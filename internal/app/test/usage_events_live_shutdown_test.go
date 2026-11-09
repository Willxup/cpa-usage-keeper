package test

import (
	"bufio"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// 应用取消时保持客户端打开，验证 SSE 先退出且正式 RunContext 的 Shutdown 成功。
func TestUsageEventsLiveStopsWithApplicationLifecycle(t *testing.T) {
	cfg := pricingRuntimeConfig(t)
	runtime := startPricingEvidenceRuntime(t, cfg)
	client := &http.Client{Timeout: 10 * time.Second}
	t.Cleanup(client.CloseIdleConnections)
	baseURL := "http://" + cfg.ListenAddress() + cfg.AppBasePath
	waitPricingEvidencePhase(t, client, baseURL, "ready")

	resp, err := client.Get(baseURL + "/api/v1/usage/events/live?range=24h")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("live status=%d content-type=%q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	reader := bufio.NewReader(resp.Body)
	var snapshot bool
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read initial snapshot: %v", err)
		}
		if line == "event: snapshot\n" {
			snapshot = true
		}
		if line == "\n" {
			break
		}
	}
	if !snapshot {
		t.Fatal("live stream did not deliver a snapshot")
	}

	runtime.cancel()
	streamDone := make(chan error, 1)
	go func() { _, err := io.Copy(io.Discard, reader); streamDone <- err }()
	select {
	case err := <-streamDone:
		if err != nil {
			t.Fatalf("live stream ended with error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("application cancellation did not end the live stream")
	}
	runtime.once.Do(func() {
		select {
		case err := <-runtime.done:
			if err != nil {
				t.Errorf("application shutdown failed: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Error("application shutdown did not finish after the live stream ended")
		}
		if err := runtime.app.Close(); err != nil {
			t.Errorf("close application: %v", err)
		}
	})
}
