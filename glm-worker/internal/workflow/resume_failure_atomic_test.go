package workflow

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestResumeProbeGateRestoreFailureIsReported(t *testing.T) {
	st := newStateStoreT(t)
	seedProviderUnavailableCheckpoint(t, st)
	resumePath := st.Path("resume-state.json")
	r := &scriptedRunner{
		steps:     []runnerStep{{structured: implementedPacket("never used")}},
		probeErrs: []error{errProbeNonTransient},
		onProbe: func() {
			if err := os.Remove(resumePath); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(resumePath, 0o700); err != nil {
				t.Fatal(err)
			}
		},
	}
	w, _ := newRecoveryWorkflowT(t, st, r)

	err := w.ExecuteResume()
	if err == nil {
		t.Fatal("errorを期待")
	}
	if !strings.Contains(err.Error(), errProbeNonTransient.Error()) {
		t.Fatalf("original probe errorが失われた: %v", err)
	}
	if !strings.Contains(err.Error(), "restore previous resume stop") {
		t.Fatalf("resume stop restore failureが報告されていない: %v", err)
	}
	if st.TaskStatus() != state.TaskStatusActive {
		t.Fatalf("失敗したrollbackを成功扱いしてstatusを変えている: %q", st.TaskStatus())
	}
}
