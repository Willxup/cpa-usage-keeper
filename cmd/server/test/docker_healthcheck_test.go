package test

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"cpa-usage-keeper/internal/api"
)

func TestDockerHealthcheckUsesReadinessWithNormalizedBasePath(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", "..", "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	lf := strings.ReplaceAll(string(content), "\r\n", "\n")
	for _, tc := range []struct{ name, content string }{
		{"LF", lf},
		{"CRLF", strings.ReplaceAll(lf, "\n", "\r\n")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testDockerHealthcheck(t, tc.content)
		})
	}
}

func testDockerHealthcheck(t *testing.T, content string) {
	t.Helper()
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("Docker healthcheck requires a POSIX shell")
	}
	// GNU wget 会改写重复斜杠与点段；这里只捕获脚本构造的 URL，再由 Go 按原路径请求。
	// 镜像内 BusyBox wget 的重定向与退出码由实际 Docker 验收覆盖。
	probeBin := t.TempDir()
	if err := os.WriteFile(filepath.Join(probeBin, "wget"), []byte("#!/bin/sh\nfor probe_url do :; done\nprintf '%s' \"$probe_url\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Windows checkout 可能使用 CRLF；先统一物理换行，再合并 Dockerfile 续行。
	content = strings.ReplaceAll(content, "\r\n", "\n")
	var probe string
	for _, line := range strings.Split(strings.ReplaceAll(content, "\\\n", " "), "\n") {
		if strings.HasPrefix(line, "HEALTHCHECK ") {
			_, probe, _ = strings.Cut(line, " CMD ")
		}
	}
	if probe == "" {
		t.Fatal("Docker healthcheck command is missing")
	}
	for _, tc := range []struct{ name, rawBasePath, basePath string }{
		{"empty", "", ""},
		{"whitespace", " \t\r\n ", ""},
		{"root", "/", ""},
		{"spaced_root", " \t/\r\n", ""},
		{"trailing_slash", "/cpa/", "/cpa"},
		{"duplicate_slashes", "//cpa///", "/cpa"},
		{"dot_segments", "/unused/../cpa/./", "/cpa"},
		{"root_dot_segments", "/unused/../", ""},
		{"spaced_dot_segments", " \t\n//unused//../cpa/./ \r\n", "/cpa"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			staticFS := fstest.MapFS{"index.html": {Data: []byte("<html>startup</html>")}}
			auth := api.AuthConfig{Enabled: true, BasePath: tc.basePath}
			shell := api.NewStartupShell(staticFS, auth, tc.basePath)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Logf("healthcheck request path: %q", r.URL.Path)
				shell.ServeHTTP(w, r)
			}))
			defer server.Close()
			_, port, err := net.SplitHostPort(server.Listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			for _, phase := range []api.StartupPhase{api.StartupPhaseOpening, api.StartupPhaseMigrating, api.StartupPhaseFailed, api.StartupPhaseReady} {
				if phase == api.StartupPhaseReady {
					if err := shell.ActivateReady(api.NewRouter(staticFS, nil, nil, nil, auth, nil, tc.basePath)); err != nil {
						t.Fatal(err)
					}
				} else if err := shell.Publish(phase, nil); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				cmd := exec.CommandContext(ctx, sh, "-c", probe)
				cmd.Env = append(os.Environ(), "APP_PORT="+port, "APP_BASE_PATH="+tc.rawBasePath, "PATH="+probeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
				output, probeErr := cmd.CombinedOutput()
				cancel()
				if probeErr != nil {
					t.Fatalf("build healthcheck URL for APP_BASE_PATH=%q: error=%v output=%s", tc.rawBasePath, probeErr, output)
				}
				client := http.Client{Timeout: 5 * time.Second}
				response, err := client.Get(string(output))
				if err != nil {
					t.Fatalf("%s healthcheck with APP_BASE_PATH=%q: %v", phase, tc.rawBasePath, err)
				}
				body, readErr := io.ReadAll(response.Body)
				_ = response.Body.Close()
				want := http.StatusServiceUnavailable
				if phase == api.StartupPhaseReady {
					want = http.StatusOK
				}
				if readErr != nil || response.StatusCode != want || !strings.Contains(string(body), `"phase":"`+string(phase)+`"`) || response.Request.URL.Path != tc.basePath+"/readyz" {
					t.Fatalf("%s healthcheck with APP_BASE_PATH=%q: status=%d final_path=%q error=%v body=%s", phase, tc.rawBasePath, response.StatusCode, response.Request.URL.Path, readErr, body)
				}
			}
		})
	}
}
