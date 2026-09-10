package workflow

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestGuardRecoveryRefCaptureSelfBlockRequestsBoundedRepairOnce(t *testing.T) {
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	stopRunner := &scriptedRunner{steps: []runnerStep{{structured: implementedPacket("done"), runErr: legacyVolatileRefError()}}}
	stopWorkflow := newGitWorkflowT(t, st, stopRunner, repo)
	if _, err := stopWorkflow.runModel(workerCheckpoint()); err == nil {
		t.Fatal("guard failure stopを期待")
	}

	oldCapture := captureCurrentGuardRecoveryRefDigest
	captureCurrentGuardRecoveryRefDigest = func(string) (string, error) {
		return "", errors.New("guard implementation cannot enumerate protected refs")
	}
	defer func() { captureCurrentGuardRecoveryRefDigest = oldCapture }()
	t.Setenv(state.GuardRepairParentActionEnv, state.GuardRepairParentActionResume)

	resumeRunner := &scriptedRunner{}
	resumeWorkflow := newGitWorkflowT(t, st, resumeRunner, repo)
	if err := resumeWorkflow.ExecuteResume(); err == nil {
		t.Fatal("self-blocking ref capture failureを期待")
	}
	first, err := st.LoadGuardRepairRecord()
	if err != nil {
		t.Fatalf("repair requestが保存されていません: %v", err)
	}
	if first.Status != state.GuardRepairRequested || first.TaskID != st.ReadOr("task.id", "") || first.RelevantDigest == "" {
		t.Fatalf("repair request = %#v", first)
	}
	if st.TaskStatus() != state.TaskStatusGuardRecoverable {
		t.Fatalf("self-block後のstatus = %s", st.TaskStatus())
	}
	if len(resumeRunner.prompts) != 0 {
		t.Fatalf("self-block中にnormal model callを実行しました: %d", len(resumeRunner.prompts))
	}

	if err := resumeWorkflow.ExecuteResume(); err == nil {
		t.Fatal("同一self-blockの再現を期待")
	}
	second, err := st.LoadGuardRepairRecord()
	if err != nil {
		t.Fatal(err)
	}
	if second.Fingerprint != first.Fingerprint || !second.UpdatedAt.Equal(first.UpdatedAt) {
		t.Fatalf("同一failure/strategy/evidenceが再要求されました: first=%#v second=%#v", first, second)
	}

	path := filepath.Join(repo, "glm-worker", "internal", "workflow", "guard_recovery.go")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("package workflow\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := resumeWorkflow.ExecuteResume(); err == nil {
		t.Fatal("source evidence変更後もcapture failureを期待")
	}
	third, err := st.LoadGuardRepairRecord()
	if err != nil {
		t.Fatal(err)
	}
	if third.Fingerprint == first.Fingerprint || third.RelevantDigest == first.RelevantDigest {
		t.Fatalf("repair-relevant source変更が新しいevidenceとして扱われません: first=%#v third=%#v", first, third)
	}
}
