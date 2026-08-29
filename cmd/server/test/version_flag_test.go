package test

import (
	"bytes"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestVersionFlagsPrintBuildVersionWithoutStartingApp(t *testing.T) {
	binaryPath := buildCLI(t)

	for _, flagName := range []string{"-v", "--version"} {
		t.Run(flagName, func(t *testing.T) {
			command := exec.Command(binaryPath, flagName)
			command.Dir = t.TempDir()
			command.Env = []string{}
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("run %s: %v\n%s", flagName, err, output)
			}
			if got := strings.TrimSpace(string(output)); got != "v9.8.7" {
				t.Fatalf("expected injected version, got %q", got)
			}
		})
	}
}

func TestHostFlagOverridesEnvFileInBuiltCLI(t *testing.T) {
	binaryPath := buildCLI(t)
	port := reserveLoopbackPort(t)
	runtimeDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(runtimeDir, "data"), 0o700); err != nil {
		t.Fatalf("create work directory: %v", err)
	}
	envPath := filepath.Join(runtimeDir, "keeper.env")
	envContent := strings.Join([]string{
		"APP_HOST=192.0.2.1",
		"APP_PORT=" + port,
		"CPA_BASE_URL=http://127.0.0.1:1",
		"CPA_MANAGEMENT_KEY=secret",
		"REDIS_QUEUE_ADDR=127.0.0.1:1",
		"AUTH_ENABLED=false",
		"WORK_DIR=./data",
		"LOG_FILE_ENABLED=false",
		"BACKUP_ENABLED=false",
		"TZ=UTC",
		"",
	}, "\n")
	if err := os.WriteFile(envPath, []byte(envContent), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	command := exec.Command(binaryPath, "--env", envPath, "--host", "127.0.0.1")
	command.Dir = runtimeDir
	command.Env = []string{}
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Start(); err != nil {
		t.Fatalf("start CLI: %v", err)
	}
	waitResult := make(chan error, 1)
	go func() {
		waitResult <- command.Wait()
	}()

	client := &http.Client{
		Transport: &http.Transport{Proxy: nil},
		Timeout:   250 * time.Millisecond,
	}
	healthURL := fmt.Sprintf("http://127.0.0.1:%s/healthz", port)
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		response, err := client.Get(healthURL)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				_ = command.Process.Kill()
				<-waitResult
				return
			}
		}

		select {
		case err := <-waitResult:
			t.Fatalf("CLI exited before binding command-line host: %v\n%s", err, output.String())
		case <-deadline.C:
			_ = command.Process.Kill()
			<-waitResult
			t.Fatalf("timeout waiting for %s\n%s", healthURL, output.String())
		case <-ticker.C:
		}
	}
}

func buildCLI(t *testing.T) string {
	t.Helper()
	repoRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	binaryPath := filepath.Join(t.TempDir(), "cpa-usage-keeper")
	if runtime.GOOS == "windows" {
		binaryPath += ".exe"
	}
	build := exec.Command(
		"go", "build",
		"-ldflags=-X cpa-usage-keeper/internal/version.Version=v9.8.7",
		"-o", binaryPath,
		"./cmd/server/main.go",
	)
	build.Dir = repoRoot
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}
	return binaryPath
}

func reserveLoopbackPort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve loopback port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("release loopback port: %v", err)
	}
	return strconv.Itoa(port)
}
