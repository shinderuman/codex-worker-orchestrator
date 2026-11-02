package workflow

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestTaskAuthoritySyncEvidenceAdmissionBoundary(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	taskPath := "IMPLEMENTATION_TASKS/active.md"
	path := filepath.Join(root, filepath.FromSlash(taskPath))
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	task := []byte("# active task\n")
	if err := os.WriteFile(path, task, 0644); err != nil {
		t.Fatal(err)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(task))
	admission := controller.Admission{
		Attempt: controller.AttemptRecord{
			AttemptID: "attempt-1",
			SemanticTaskRef: controller.SemanticTaskRef{
				TaskPath: taskPath, ContractDigest: digest,
			},
		},
		Workspace: controller.WorkspaceIdentity{Root: root},
	}
	binding := state.ControllerRuntimeBinding{
		AttemptID: "attempt-1", TaskPath: taskPath, TaskContractDigest: digest,
	}
	attached := TaskAuthoritySyncEvidence{
		Runtime: TaskAuthorityRuntimeAttached,
		TaskID:  "task-1",
		Binding: &binding,
	}
	check := func(t *testing.T, evidence TaskAuthoritySyncEvidence, wantError bool) {
		t.Helper()
		payload, err := json.Marshal(evidence)
		if err != nil {
			t.Fatal(err)
		}
		err = VerifyTaskAuthoritySyncRuntime(payload, admission)
		if (err != nil) != wantError {
			t.Fatalf("VerifyTaskAuthoritySyncRuntime(%+v): error=%v, wantError=%v", evidence, err, wantError)
		}
	}

	t.Run("attached exact attempt task and disk contract", func(t *testing.T) {
		check(t, attached, false)
	})
	t.Run("unstarted runtime requires no attached binding", func(t *testing.T) {
		check(t, TaskAuthoritySyncEvidence{Runtime: TaskAuthorityRuntimeUnstarted}, false)
	})
	for _, tc := range []struct {
		name   string
		mutate func(*TaskAuthoritySyncEvidence)
	}{
		{"foreign attempt", func(e *TaskAuthoritySyncEvidence) { e.Binding.AttemptID = "foreign" }},
		{"foreign task", func(e *TaskAuthoritySyncEvidence) { e.Binding.TaskPath = "IMPLEMENTATION_TASKS/other.md" }},
		{"foreign digest", func(e *TaskAuthoritySyncEvidence) { e.Binding.TaskContractDigest = "foreign" }},
		{"missing binding", func(e *TaskAuthoritySyncEvidence) { e.Binding = nil }},
		{"unknown runtime phase", func(e *TaskAuthoritySyncEvidence) { e.Runtime = "unknown" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := attached
			claimed := binding
			candidate.Binding = &claimed
			tc.mutate(&candidate)
			check(t, candidate, true)
		})
	}
	t.Run("task file changed after evidence", func(t *testing.T) {
		if err := os.WriteFile(path, []byte("# modified task\n"), 0644); err != nil {
			t.Fatal(err)
		}
		check(t, attached, true)
	})
	t.Run("malformed evidence is rejected", func(t *testing.T) {
		if err := VerifyTaskAuthoritySyncRuntime([]byte("{"), admission); err == nil {
			t.Fatal("malformed transition evidence was accepted")
		}
	})
}
