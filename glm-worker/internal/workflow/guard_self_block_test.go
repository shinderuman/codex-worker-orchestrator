package workflow

import (
	"errors"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestGuardRecoveryRefCaptureFailsClosedWithoutRepairContext(t *testing.T) {
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

	resumeRunner := &scriptedRunner{}
	resumeWorkflow := newGitWorkflowT(t, st, resumeRunner, repo)
	err := resumeWorkflow.ExecuteResume()
	if err == nil {
		t.Fatal("self-blocking ref capture failureを期待")
	}
	if !strings.Contains(err.Error(), "guard recovery cannot capture current refs") {
		t.Fatalf("original guard recovery failureが保持されていません: %v", err)
	}
	if st.TaskStatus() != state.TaskStatusGuardRecoverable {
		t.Fatalf("self-block後のstatus = %s", st.TaskStatus())
	}
	if len(resumeRunner.prompts) != 0 {
		t.Fatalf("self-block中にnormal model callを実行しました: %d", len(resumeRunner.prompts))
	}
	if st.Exists("guard-repair.json") {
		t.Fatal("guard recovery failureが独立repair stateを生成しました")
	}
}
