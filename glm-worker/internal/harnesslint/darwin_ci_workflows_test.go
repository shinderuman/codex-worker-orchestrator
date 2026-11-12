package harnesslint

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

type darwinCIWorkflow struct {
	On struct {
		PullRequest struct {
			Paths []string `yaml:"paths"`
		} `yaml:"pull_request"`
		Push struct {
			Branches []string `yaml:"branches"`
			Paths    []string `yaml:"paths"`
		} `yaml:"push"`
	} `yaml:"on"`
	Jobs map[string]struct {
		Steps []struct {
			Uses            string            `yaml:"uses"`
			Run             string            `yaml:"run"`
			Env             map[string]string `yaml:"env"`
			ContinueOnError bool              `yaml:"continue-on-error"`
			With            struct {
				PersistCredentials *bool `yaml:"persist-credentials"`
			} `yaml:"with"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

func readDarwinCIWorkflow(t *testing.T, filename string) darwinCIWorkflow {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "..", ".github", "workflows", filename))
	if err != nil {
		t.Fatal(err)
	}
	var workflow darwinCIWorkflow
	if err := yaml.Unmarshal(body, &workflow); err != nil {
		t.Fatal(err)
	}
	if len(workflow.On.PullRequest.Paths) == 0 || len(workflow.On.Push.Paths) == 0 {
		t.Fatalf("%s must filter both pull_request and push", filename)
	}
	if !reflect.DeepEqual(workflow.On.PullRequest.Paths, workflow.On.Push.Paths) {
		t.Fatalf("%s has different pull_request and push path filters", filename)
	}
	if !containsDarwinCIPath(workflow.On.Push.Branches, "web-gpt/**") {
		t.Fatalf("%s cannot execute on the Web GPT branch push", filename)
	}
	if len(workflow.Jobs) != 1 {
		t.Fatalf("%s expected one Darwin job, got %d", filename, len(workflow.Jobs))
	}
	for name, job := range workflow.Jobs {
		checkouts := 0
		for _, step := range job.Steps {
			if step.ContinueOnError {
				t.Fatalf("%s job %s must propagate test and smoke failures", filename, name)
			}
			if !strings.HasPrefix(step.Uses, "actions/checkout@") {
				continue
			}
			checkouts++
			if step.With.PersistCredentials == nil || *step.With.PersistCredentials {
				t.Fatalf("%s job %s must set checkout persist-credentials: false", filename, name)
			}
		}
		if checkouts != 1 {
			t.Fatalf("%s job %s expected exactly one checkout, got %d", filename, name, checkouts)
		}
	}
	return workflow
}

func containsDarwinCIPath(paths []string, path string) bool {
	for _, item := range paths {
		if item == path {
			return true
		}
	}
	return false
}

func darwinCIPathMatches(pattern, changed string) bool {
	if strings.HasSuffix(pattern, "/**") {
		return strings.HasPrefix(changed, strings.TrimSuffix(pattern, "/**")+"/")
	}
	return pattern == changed
}

func darwinCIPathTriggered(patterns []string, changed string) bool {
	for _, pattern := range patterns {
		if darwinCIPathMatches(pattern, changed) {
			return true
		}
	}
	return false
}

func TestDarwinCIWorkflowPathClosure(t *testing.T) {
	cases := []struct {
		workflow string
		positive []string
		negative []string
	}{
		{
			workflow: "observation-darwin.yml",
			positive: []string{
				"glm-worker/internal/observationexec/execute_darwin.go",
				"glm-worker/internal/qualitygate/resource_samples_darwin.go",
				"glm-worker/internal/execfile/execfile.go",
			},
			negative: []string{
				"README.md",
				"glm-worker/internal/uninstallcmd/run.go",
				"glm-worker/internal/qualitygate-other/sample.go",
			},
		},
		{
			workflow: "uninstall-darwin.yml",
			positive: []string{
				"glm-worker/internal/uninstallcmd/run.go",
				"glm-worker/internal/codexinstall/retire.go",
				"glm-worker/internal/cliinstall/retire.go",
				"glm-worker/internal/installlock/lock.go",
				"glm-worker/internal/claudesettings/location.go",
				"glm-worker/internal/pathalias/path.go",
				"glm-worker/internal/settingsmerge/merge.go",
				"glm-worker/internal/mergejsoncmd/run.go",
				"glm-worker/internal/claudeoverride/override.go",
				"scripts/manage-pull-hook.sh",
				".githooks/pre-commit",
				"install.sh",
				"uninstall.sh",
				"tests/install_smoke.sh",
				"claude/settings-managed.json",
				"codex/AGENTS.md",
				"quality-tools.yml",
				"install-quality-tools.sh",
				"glm-worker/go.mod",
				"go.work",
			},
			negative: []string{
				"README.md",
				"glm-worker/internal/qualitygate/resource_samples_darwin.go",
				"scripts/manage-pull-hook.sh.bak",
				"glm-worker/internal/codexinstall-other/retire.go",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.workflow, func(t *testing.T) {
			workflow := readDarwinCIWorkflow(t, tc.workflow)
			for _, changed := range tc.positive {
				if !darwinCIPathTriggered(workflow.On.PullRequest.Paths, changed) {
					t.Errorf("required change %s does not trigger %s", changed, tc.workflow)
				}
			}
			for _, changed := range tc.negative {
				if darwinCIPathTriggered(workflow.On.PullRequest.Paths, changed) {
					t.Errorf("unrelated change %s unexpectedly triggers %s", changed, tc.workflow)
				}
			}
		})
	}
}

func TestDarwinCIWorkflowExecution(t *testing.T) {
	observation := readDarwinCIWorkflow(t, "observation-darwin.yml")
	testCommand := "go test -count=1 ./internal/observationexec ./internal/qualitygate"
	var found bool
	for _, job := range observation.Jobs {
		for _, step := range job.Steps {
			if strings.TrimSpace(step.Run) == testCommand {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("Darwin observation job must execute both observationexec and qualitygate: %s", testCommand)
	}

	uninstall := readDarwinCIWorkflow(t, "uninstall-darwin.yml")
	var testRun, raceRun, varSmoke, privateSmoke bool
	for _, job := range uninstall.Jobs {
		for _, step := range job.Steps {
			switch strings.TrimSpace(step.Run) {
			case "go test -count=1 ./internal/uninstallcmd":
				testRun = true
			case "go test -race -count=1 ./internal/uninstallcmd":
				raceRun = true
			case "./tests/install_smoke.sh":
				switch step.Env["TMPDIR"] {
				case "/private/tmp":
					privateSmoke = true
				case "":
					varSmoke = true
				}
			}
		}
	}
	if !testRun || !raceRun || !varSmoke || !privateSmoke {
		t.Fatalf("Darwin uninstall steps incomplete: test=%t race=%t default-temp-smoke=%t private-tmp-smoke=%t", testRun, raceRun, varSmoke, privateSmoke)
	}
}
