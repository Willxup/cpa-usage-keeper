package test

import (
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type releaseWorkflow struct {
	On   map[string]yaml.Node `yaml:"on"`
	Jobs map[string]struct {
		Uses  string         `yaml:"uses"`
		Needs yaml.Node      `yaml:"needs"`
		With  map[string]any `yaml:"with"`
		Steps []struct {
			Run  string            `yaml:"run"`
			Uses string            `yaml:"uses"`
			With map[string]string `yaml:"with"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

func TestReleaseDownloadsOnlyCurrentBinaryPackages(t *testing.T) {
	workflow := readReleaseWorkflow(t, "binary-release.yml")
	const tag = "v1.15.12"
	for _, jobID := range []string{"publish-release", "update-homebrew-tap"} {
		t.Run(jobID, func(t *testing.T) {
			downloads := 0
			for _, step := range workflow.Jobs[jobID].Steps {
				if !strings.HasPrefix(step.Uses, "actions/download-artifact@") {
					continue
				}
				downloads++
				pattern := strings.ReplaceAll(step.With["pattern"], "${{ github.ref_name }}", tag)
				if pattern == "" || step.With["name"] != "" || step.With["artifact-ids"] != "" {
					t.Fatal("release downloads must filter artifacts by the current version's binary package names")
				}
				check := func(name string, want bool) {
					t.Helper()
					matched, err := path.Match(pattern, name)
					if err != nil || matched != want {
						t.Errorf("pattern %q matches %q = %t, want %t (err=%v)", pattern, name, matched, want, err)
					}
				}
				for _, platform := range []string{"linux", "darwin", "windows"} {
					for _, arch := range []string{"amd64", "arm64"} {
						check("cpa-usage-keeper-"+tag+"-"+platform+"-"+arch, true)
					}
				}
				for _, unrelated := range []string{
					"willxup~cpa-usage-keeper~build123.dockerbuild",
					"cpa-usage-keeper-v1.15.11-darwin-arm64",
					"frontend-test-results",
				} {
					check(unrelated, false)
				}
			}
			if downloads != 1 {
				t.Fatalf("expected one filtered download, got %d", downloads)
			}
		})
	}
}

func readReleaseWorkflow(t *testing.T, name string) releaseWorkflow {
	t.Helper()
	path := filepath.Join("..", "..", "..", ".github", "workflows", name)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var workflow releaseWorkflow
	if err := yaml.Unmarshal(content, &workflow); err != nil {
		t.Fatalf("decode %s: %v", name, err)
	}
	return workflow
}

func TestCIHasNoBranchPushTrigger(t *testing.T) {
	workflow := readReleaseWorkflow(t, "ci.yml")
	if _, ok := workflow.On["push"]; ok {
		t.Fatal("CI must not rerun on branch pushes after a PR merge")
	}
	for _, event := range []string{"pull_request", "workflow_dispatch", "workflow_call"} {
		if _, ok := workflow.On[event]; !ok {
			t.Errorf("CI is missing %s", event)
		}
	}
}

func TestReleasePublishersWaitForOneFullCI(t *testing.T) {
	workflow := readReleaseWorkflow(t, "release.yml")
	var push struct {
		Branches []string `yaml:"branches"`
		Tags     []string `yaml:"tags"`
	}
	trigger := workflow.On["push"]
	if err := trigger.Decode(&push); err != nil {
		t.Fatal(err)
	}
	if len(push.Branches) != 0 || len(push.Tags) != 1 || push.Tags[0] != "v*" {
		t.Fatalf("release must only trigger for version tags: %+v", push)
	}
	ciCalls := 0
	for _, job := range workflow.Jobs {
		if job.Uses == "./.github/workflows/ci.yml" {
			ciCalls++
		}
	}
	if ci := workflow.Jobs["ci"]; ciCalls != 1 || ci.Uses != "./.github/workflows/ci.yml" || ci.With["full"] != true {
		t.Fatal("release must run exactly one complete CI matrix")
	}
	for _, name := range []string{"docker-publish.yml", "binary-release.yml"} {
		publisher := readReleaseWorkflow(t, name)
		if _, callable := publisher.On["workflow_call"]; !callable || len(publisher.On) != 1 {
			t.Errorf("%s has a trigger that can bypass release CI", name)
		}
		calls := 0
		for id, job := range workflow.Jobs {
			if job.Uses != "./.github/workflows/"+name {
				continue
			}
			calls++
			var dependency string
			if err := job.Needs.Decode(&dependency); err != nil || dependency != "ci" {
				t.Errorf("publisher %s must depend on CI success", id)
			}
		}
		if calls != 1 {
			t.Errorf("expected one call to %s, got %d", name, calls)
		}
	}
}

func TestCIGateRejectsIncompleteReleaseChecks(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is required to exercise the GitHub Actions gate")
	}
	workflow := readReleaseWorkflow(t, "ci.yml")
	gate := workflow.Jobs["ci-gate"]
	if len(gate.Steps) != 1 || gate.Steps[0].Run == "" {
		t.Fatal("missing CI gate script")
	}
	runGate := func(t *testing.T, full bool, results [4]string, wantPass bool) {
		t.Helper()
		command := exec.Command(bash, "-e", "-c", strings.ReplaceAll(gate.Steps[0].Run, "\r\n", "\n"))
		fullValue := "false"
		if full {
			fullValue = "true"
		}
		command.Env = append(os.Environ(),
			"FULL_CHECK="+fullValue,
			"CHANGES_RESULT="+results[0],
			"BACKEND_RESULT="+results[1],
			"FRONTEND_RESULT="+results[2],
			"CONTAINER_RESULT="+results[3],
		)
		output, err := command.CombinedOutput()
		if (err == nil) != wantPass {
			t.Fatalf("full=%t results=%v: want pass=%t, err=%v\n%s", full, results, wantPass, err, output)
		}
	}
	for _, full := range []bool{false, true} {
		runGate(t, full, [4]string{"success", "success", "success", "success"}, true)
		for i, job := range []string{"changes", "backend", "frontend", "container"} {
			for _, result := range []string{"failure", "cancelled", "skipped"} {
				t.Run(job+"/"+result+"/full="+map[bool]string{false: "false", true: "true"}[full], func(t *testing.T) {
					results := [4]string{"success", "success", "success", "success"}
					results[i] = result
					runGate(t, full, results, !full && i > 0 && result == "skipped")
				})
			}
		}
	}
}
