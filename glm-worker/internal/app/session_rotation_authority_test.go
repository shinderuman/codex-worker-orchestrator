package app

import (
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestCanonicalSessionRotationEvaluationDoesNotRequireTaskStats(t *testing.T) {
	for _, fixture := range []string{"missing", "corrupt"} {
		t.Run(fixture, func(t *testing.T) {
			root := t.TempDir()
			cfg := config.AppConfig{
				StateBase:      filepath.Join(root, "state"),
				RepoHash:       "session-rotation-authority",
				CodexConfigDir: filepath.Join(root, "missing-codex-home"),
				CodexBin:       filepath.Join(root, "missing-codex-bin"),
			}
			st, err := state.NewStateStore(cfg)
			if err != nil {
				t.Fatal(err)
			}
			taskID := "12345678-aaaa-bbbb-cccc-dddddddddddd"
			threadID := "01a0463c-d477-7410-9efd-cb34ff2e0b0e"
			sessionID := "01a0463c-d477-7410-9efd-cb34ff2e0b0f"
			if err := st.Write("task.id", taskID); err != nil {
				t.Fatal(err)
			}
			if err := st.SetTaskStatus(state.TaskStatusActive); err != nil {
				t.Fatal(err)
			}
			if err := st.SetParentCodexIdentity(threadID, sessionID, nil); err != nil {
				t.Fatal(err)
			}
			if fixture == "corrupt" {
				if err := st.Write("task-stats.json", "{broken\n"); err != nil {
					t.Fatal(err)
				}
			}

			evaluation, err := EvaluateCanonicalSessionRotationTerminal(cfg, st, state.SessionRotationTerminalAccept, "LOW")
			if err != nil {
				t.Fatal(err)
			}
			if evaluation.TaskID != taskID || evaluation.ParentThreadID != threadID {
				t.Fatalf("canonical identity = task:%q thread:%q", evaluation.TaskID, evaluation.ParentThreadID)
			}
			if !evaluation.AcceptedTasksUnavailable {
				t.Fatalf("unknown accepted-task history was treated as available: %#v", evaluation)
			}
			if !evaluation.Decision.Required || evaluation.Decision.Reason != state.SessionRotationReasonEvidenceUnavailable {
				t.Fatalf("unknown accepted-task history was not explicit evidence-unavailable: %#v", evaluation.Decision)
			}
			if !hasCanonicalSessionRotationEvidence(evaluation.Decision.Evidence, state.SessionRotationReasonEvidenceUnavailable, state.SessionRotationEvidenceFieldAcceptedTasks, st.SessionRotationMarkerPath(threadID)) {
				t.Fatalf("accepted-task provenance missing from decision evidence: %#v", evaluation.Decision.Evidence)
			}
		})
	}
}

func hasCanonicalSessionRotationEvidence(evidence []state.SessionRotationEvidence, trigger, field, source string) bool {
	for _, item := range evidence {
		if item.Trigger == trigger && item.Field == field && item.Source == source {
			return true
		}
	}
	return false
}
