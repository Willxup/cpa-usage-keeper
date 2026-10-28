package cpa_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"cpa-usage-keeper/internal/cpa"
	"cpa-usage-keeper/internal/cpa/dto/providerconfig"
)

func TestProviderConfigETagRoundTrip(t *testing.T) {
	for _, provider := range []string{"codex", "meta", "openai"} {
		for _, etag := range []string{`"config-sha256"`, ""} {
			t.Run(provider+"/"+etag, func(t *testing.T) {
				var reads, writes atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Header.Get("Authorization") != "Bearer management-secret" {
						t.Error("management authorization missing")
					}
					switch r.Method {
					case http.MethodGet:
						reads.Add(1)
						w.Header().Set("ETag", etag)
						fmt.Fprint(w, `[{"keys":[{"api-key":"key","auth_index":"target"}]}]`)
					case http.MethodPut:
						writes.Add(1)
						if got := r.Header.Get("If-Match"); got != etag {
							t.Errorf("If-Match = %q, want %q", got, etag)
						}
						if etag == "" && len(r.Header.Values("If-Match")) != 0 {
							t.Error("old CPA must receive no If-Match header")
						}
					default:
						t.Errorf("unexpected method %s", r.Method)
					}
				}))
				defer server.Close()
				client := cpa.NewClient(server.URL, "management-secret", time.Second, false)
				document, token, err := fetchProviderConfigETag(client, provider)
				if err != nil || token != etag || document == nil {
					t.Fatalf("GET token = %q, document = %v, error = %v", token, document, err)
				}
				if _, err := client.UpdateProviderConfig(context.Background(), provider, document, token); err != nil {
					t.Fatal(err)
				}
				if reads.Load() != 1 || writes.Load() != 1 {
					t.Fatalf("reads = %d, writes = %d", reads.Load(), writes.Load())
				}
			})
		}
	}
}

// 同一路径的较早请求晚返回，也必须拿到自己的 token，而不是客户端最后看到的头。
func TestProviderConfigConcurrentReadsKeepTheirOwnETags(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	firstRead, release := make(chan struct{}), make(chan struct{})
	var reads, writes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			n := reads.Add(1)
			w.Header().Set("ETag", fmt.Sprintf(`"version-%d"`, n))
			if n == 1 {
				close(firstRead)
				select {
				case <-release:
				case <-ctx.Done():
					return
				}
			}
			fmt.Fprintf(w, `[{"keys":[{"api-key":"key-%d","auth_index":"target"}]}]`, n)
			return
		}
		writes.Add(1)
		var document providerconfig.Document
		if err := json.NewDecoder(r.Body).Decode(&document); err != nil {
			t.Error(err)
			return
		}
		key := string(document.Groups[0].Keys[0].Fields["api-key"])
		want := `"version-1"`
		if key == `"key-2"` {
			want = `"version-2"`
		}
		if got := r.Header.Get("If-Match"); got != want {
			t.Errorf("key %s: If-Match = %q, want %q", key, got, want)
		}
	}))
	defer server.Close()
	client := cpa.NewClient(server.URL, "management-secret", time.Second, false)
	done := make(chan error, 1)
	go func() {
		result, err := client.FetchProviderKeyConfig(ctx, "codex")
		if err == nil {
			_, err = client.UpdateProviderConfig(ctx, "codex", result.Document, result.ETag)
		}
		done <- err
	}()
	select {
	case <-firstRead:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	second, err := client.FetchProviderKeyConfig(ctx, "codex")
	close(release)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.UpdateProviderConfig(ctx, "codex", second.Document, second.ETag); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if reads.Load() != 2 || writes.Load() != 2 {
		t.Fatalf("reads = %d, writes = %d", reads.Load(), writes.Load())
	}
}

func fetchProviderConfigETag(client *cpa.Client, provider string) (*providerconfig.Document, string, error) {
	if provider == "openai" {
		result, err := client.FetchOpenAICompatibility(context.Background())
		return result.Document, result.ETag, err
	}
	if provider == "meta" {
		result, err := client.FetchPriorityProviderConfig(context.Background(), provider)
		return result.Document, result.ETag, err
	}
	result, err := client.FetchProviderKeyConfig(context.Background(), provider)
	return result.Document, result.ETag, err
}
