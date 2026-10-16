package test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type restoreReleaseWorkflow struct {
	Jobs map[string]struct {
		Steps []struct {
			Name string `yaml:"name"`
			Run  string `yaml:"run"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

// 发行契约跨 stable/dev 和全部平台，防止只修一个发布路径导致用户拿不到离线恢复工具。
func TestReleasePackagesIncludeRestoreToolForEveryPlatform(t *testing.T) {
	for _, tc := range []struct{ file, prefix string }{
		{"binary-release.yml", "binary-release-"},
		{"binary-dev-release.yml", "binary-dev-"},
	} {
		content, err := os.ReadFile(filepath.Join("..", "..", "..", ".github", "workflows", tc.file))
		if err != nil {
			t.Fatal(err)
		}
		var workflow restoreReleaseWorkflow
		if err := yaml.Unmarshal(content, &workflow); err != nil {
			t.Fatal(err)
		}
		for _, platform := range []string{"linux", "darwin", "windows"} {
			t.Run(tc.file+"/"+platform, func(t *testing.T) {
				job, ok := workflow.Jobs[tc.prefix+platform]
				if !ok {
					t.Fatal("release platform job is missing")
				}
				executable := "pricing-restore"
				if platform == "windows" {
					executable += ".exe"
				}
				found := false
				for _, step := range job.Steps {
					if step.Name != "Build binary package" {
						continue
					}
					for _, line := range strings.Split(strings.ReplaceAll(step.Run, "\\\n", " "), "\n") {
						if !strings.Contains(line, "./cmd/pricing-restore") {
							continue
						}
						found = true
						for _, required := range []string{"CGO_ENABLED=1", `GOOS="${GOOS}"`, `GOARCH="${GOARCH}"`, "go build", `-o "${package_dir}/` + executable + `"`} {
							if !strings.Contains(line, required) {
								t.Fatalf("restore build lacks %s: %s", required, line)
							}
						}
						if platform != "darwin" && !strings.Contains(line, `CC="${CC}"`) {
							t.Fatalf("restore build lost cross compiler: %s", line)
						}
					}
				}
				if !found {
					t.Fatal("release archive does not build a restore tool in its package directory")
				}
			})
		}
		if tc.file == "binary-release.yml" {
			if !strings.Contains(string(content), `bin.install "cpa-usage-keeper", "pricing-restore"`) {
				t.Fatal("generated Homebrew formula does not install restore tool")
			}
		}
	}
}

func TestDockerImageBuildsAndIncludesRestoreTool(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", "..", "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	build := strings.ReplaceAll(string(content), "\\\n", " ")
	if !strings.Contains(build, "-o /out/pricing-restore ./cmd/pricing-restore") || !strings.Contains(build, "COPY --from=go-builder /out/pricing-restore /app/pricing-restore") {
		t.Fatal("Docker final image does not build and copy restore tool")
	}
}
