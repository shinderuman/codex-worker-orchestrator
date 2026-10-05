package parentcontinuation

import (
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestObservationCapabilityAdmissionComposesRepositoryPolicyWithLifecycle(t *testing.T) {
	st := observationCapabilityFixture(t, "# observation\n\n## External feasibility\n\nstatus: observation\nassumption: representative producer behavior\n")
	admission, err := CurrentObservationCapabilityAdmission(st)
	if err != nil {
		t.Fatal(err)
	}
	if !admission.Policy.Admitted || admission.Policy.Status != "observation" || admission.Policy.AuthorityDigest == "" {
		t.Fatalf("admission = %#v", admission)
	}
	plan, err := st.ParentActionPlan()
	if err != nil {
		t.Fatal(err)
	}
	if plan.Allows(state.ParentActionObservationExecute) || plan.Allows(state.ParentActionNoGo) {
		t.Fatalf("generic state plan contains repository policy actions: %#v", plan)
	}
	projected := projectObservationCapabilityActions(plan, admission.Policy.Admitted)
	if !projected.Allows(state.ParentActionObservationExecute) || !projected.Allows(state.ParentActionNoGo) {
		t.Fatalf("repository policy actions were not projected: %#v", projected)
	}
}

func TestObservationCapabilityAdmissionRejectsNonObservationPolicy(t *testing.T) {
	for _, declaration := range []string{
		"# implementation\n\n## External feasibility\n\nstatus: implementation\nassumption: producer behavior\nevidence-source: producer\nevidence: observed\ngo: approved\n",
		"# normal\n\n## External feasibility\n\nstatus: not-applicable\n",
		"# malformed\n",
	} {
		st := observationCapabilityFixture(t, declaration)
		if _, err := CurrentObservationCapabilityAdmission(st); err == nil {
			t.Fatalf("non-observation policy was admitted: %q", declaration)
		}
	}
}

func TestObservationCapabilityAdmissionInvalidatesOnAuthorityChange(t *testing.T) {
	st := observationCapabilityFixture(t, "# observation\n\n## External feasibility\n\nstatus: observation\nassumption: producer behavior\n")
	expected, err := CurrentObservationCapabilityAdmission(st)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveCurrentTaskAuthority("IMPLEMENTATION_TASKS/observation.md", []byte("# observation changed\n\n## External feasibility\n\nstatus: observation\nassumption: producer behavior changed\n")); err != nil {
		t.Fatal(err)
	}
	if err := ValidateObservationCapabilityAdmission(st, expected); err == nil {
		t.Fatal("changed task authority retained stale observation capability")
	}
}

func observationCapabilityFixture(t *testing.T, declaration string) *state.StateStore {
	t.Helper()
	repoRoot := t.TempDir()
	cfg := config.AppConfig{RepoRoot: repoRoot, RepoHash: config.RepoHashFor(repoRoot), StateBase: t.TempDir()}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveCurrentTaskAuthority("IMPLEMENTATION_TASKS/observation.md", []byte(declaration)); err != nil {
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
	return st
}
