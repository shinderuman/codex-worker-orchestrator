package app

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentactiongrammar"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestFreshCanonicalHandoffUsesCommittedAuthorityWithoutRuntime(t *testing.T) {
	for _, scenario := range []string{"fresh", "missing-task", "unfinished-runtime"} {
		t.Run(scenario, func(t *testing.T) {
			cfg, task, blocker, _ := newCanonicalWorkflowCorpus(t)
			body, err := os.ReadFile(filepath.Join(cfg.RepoRoot, task))
			if err != nil {
				t.Fatal(err)
			}
			writeAppTestFile(t, cfg.RepoRoot, task, strings.Replace(string(body), "- `"+blocker+"`", "none", 1))
			if scenario == "missing-task" {
				if err := os.Remove(filepath.Join(cfg.RepoRoot, task)); err != nil {
					t.Fatal(err)
				}
			}
			runControllerActivationGit(t, cfg.RepoRoot, "add", "--all")
			runControllerActivationGit(t, cfg.RepoRoot, "commit", "-qm", "initial task authority")
			cfg, err = controller.WorkflowConfig(cfg)
			if err != nil {
				t.Fatal(err)
			}
			runtime := state.AttachStateStore(cfg)
			if scenario == "unfinished-runtime" {
				runtime, err = state.NewStateStore(cfg)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := runtime.StartNewTask(); err != nil {
					t.Fatal(err)
				}
			}
			var output bytes.Buffer
			if err := Execute(Command{Mode: ModeHandoff}, cfg, nil, &output, io.Discard); err != nil {
				t.Fatal(err)
			}
			var handoff struct {
				parentHandoffOutput
				Specs map[string]parentHandoffActionSpec `json:"action_specs"`
			}
			if err := json.Unmarshal(output.Bytes(), &handoff); err != nil {
				t.Fatal(err)
			}
			if scenario == "fresh" {
				if !handoff.Consistent || handoff.Specs[parentactiongrammar.StartAction].Kind != "direct" || runtime.Exists("task.id") {
					t.Fatalf("fresh handoff did not admit ordinary start without initializing Runtime: %s", output.String())
				}
			} else if containsParentAction(handoff.AllowedActions, parentactiongrammar.StartAction) || (scenario == "missing-task" && handoff.Consistent) {
				t.Fatalf("unavailable authority or unfinished Runtime admitted a fresh start: %s", output.String())
			}
			if scenario == "unfinished-runtime" && runtime.TaskStatus() != state.TaskStatusActive {
				t.Fatal("handoff changed the unfinished Runtime")
			}
			if exists, err := controller.Exists(cfg); err != nil || exists {
				t.Fatalf("read-only handoff bootstrapped a controller: %v", err)
			}
		})
	}
}
