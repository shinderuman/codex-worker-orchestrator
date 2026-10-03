package parentactioncmd

import (
	"bytes"

	"errors"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repolock"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestRunNoGoRejectsHeldRepositoryLock(t *testing.T) {
	cfg, st := newParentActionIdentityTestState(t)
	if err := st.SaveCurrentTaskAuthority("IMPLEMENTATION_TASKS/observation.md", []byte("# observation\n\n## External feasibility\n\nstatus: observation\nassumption: representative producer behavior\n")); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusWaitingDecision); err != nil {
		t.Fatal(err)
	}
	if err := st.Touch("pending-decision"); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordSolResult(packet.Result{Status: packet.StatusNeedsSolDecision, Risk: packet.RiskHigh}, state.ParentReviewProducer{Role: string(state.WorkerRole), Model: "opus"}); err != nil {
		t.Fatal(err)
	}

	lock, err := repolock.Acquire(st.LockPath())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Close() }()

	if err := runNoGo(cfg, &bytes.Buffer{}); !errors.Is(err, repolock.ErrRepoLockHeld) {
		t.Fatalf("runNoGo error = %v, want ErrRepoLockHeld", err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingDecision || !st.Exists("pending-decision") {
		t.Fatalf("contended no-go mutated state: status:%s pending:%v", st.TaskStatus(), st.Exists("pending-decision"))
	}
}

func TestRunNoGoRejectsGenericDecision(t *testing.T) {
	cfg, st := newParentActionIdentityTestState(t)
	if err := st.SaveCurrentTaskAuthority("IMPLEMENTATION_TASKS/normal.md", []byte("# normal\n\n## External feasibility\n\nstatus: not-applicable\n")); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusWaitingDecision); err != nil {
		t.Fatal(err)
	}
	if err := st.Touch("pending-decision"); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordSolResult(packet.Result{Status: packet.StatusNeedsSolDecision, Risk: packet.RiskHigh}, state.ParentReviewProducer{}); err != nil {
		t.Fatal(err)
	}

	if err := execute(cfg, []string{"no-go"}, &bytes.Buffer{}, nil); err == nil {
		t.Fatal("generic decision accepted terminal no-go")
	}
	if st.TaskStatus() != state.TaskStatusWaitingDecision || !st.Exists("pending-decision") {
		t.Fatalf("generic decision was mutated: status:%s pending:%v", st.TaskStatus(), st.Exists("pending-decision"))
	}
}
