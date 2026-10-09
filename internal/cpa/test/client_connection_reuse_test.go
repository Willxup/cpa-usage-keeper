package cpa_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cpa-usage-keeper/internal/cpa"
)

func TestClientReusesConnectionsAcrossConcurrentMetadataRequests(t *testing.T) {
	const concurrency = 7
	var connections atomic.Int32
	var requests atomic.Int32
	gates := []chan struct{}{make(chan struct{}), make(chan struct{})}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		arrival := int(requests.Add(1))
		wave := (arrival - 1) / concurrency
		if arrival%concurrency == 0 {
			close(gates[wave])
		}
		// 每轮等全部请求到达再响应，确保真正使用七条并发连接。
		select {
		case <-gates[wave]:
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"files":[]}`)
		case <-r.Context().Done():
		}
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	server.Start()
	defer server.Close()
	client := cpa.NewClient(server.URL, "test-management-key", 5*time.Second, false)

	for wave := 0; wave < len(gates); wave++ {
		var wg sync.WaitGroup
		for range concurrency {
			wg.Go(func() {
				if _, err := client.FetchAuthFiles(context.Background()); err != nil {
					t.Errorf("fetch auth files: %v", err)
				}
			})
		}
		wg.Wait()
		if got := connections.Load(); got != concurrency {
			t.Fatalf("wave %d opened %d connections; want reuse of the original %d", wave+1, got, concurrency)
		}
	}
}
