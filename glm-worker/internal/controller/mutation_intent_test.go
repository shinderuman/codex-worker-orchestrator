package controller

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type qualityIntentFixture struct {
	repo      string
	other     string
	store     *Store
	workspace WorkspaceIdentity
	before    WorkspaceSnapshot
	admission Admission
}

func newQualityIntentFixture(t *testing.T) *qualityIntentFixture {
	t.Helper()
	repo, other := newControllerLinkedWorktree(t)
	store, err := Open(controllerTestConfig(repo, filepath.Join(t.TempDir(), "state", "sessions")))
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := ResolveWorkspaceIdentity(repo, store.Identity())
	if err != nil {
		t.Fatal(err)
	}
	before, err := CaptureWorkspaceSnapshot(repo)
	if err != nil {
		t.Fatal(err)
	}
	task := controllerTestTask(t, repo)
	admission, err := store.BootstrapExecution(task, workspace, before)
	if err != nil {
		t.Fatal(err)
	}
	return &qualityIntentFixture{repo: repo, other: other, store: store, workspace: workspace, before: before, admission: admission}
}

func (f *qualityIntentFixture) authority() MutationAuthority {
	return f.admission.MutationAuthority()
}

func (f *qualityIntentFixture) currentAuthority(t *testing.T) MutationAuthority {
	t.Helper()
	authority, err := f.store.currentHeadAuthority()
	if err != nil {
		t.Fatal(err)
	}
	return authority
}

func (f *qualityIntentFixture) stageQualityIntent(t *testing.T) {
	t.Helper()
	if err := f.store.StageQualityMutation(f.admission); err != nil {
		t.Fatal(err)
	}
}

func (f *qualityIntentFixture) applyQualityOutput(t *testing.T) WorkspaceSnapshot {
	t.Helper()
	target := filepath.Join(f.repo, "fixture.go")
	if err := os.WriteFile(target, []byte("package fixture\n\nvar formatted = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	output, err := CaptureWorkspaceSnapshot(f.repo)
	if err != nil {
		t.Fatal(err)
	}
	if output.ID == f.before.ID {
		t.Fatal("fixture output snapshot did not change")
	}
	return output
}

func (f *qualityIntentFixture) preserveQualityOutput(t *testing.T, output WorkspaceSnapshot) {
	t.Helper()
	provenance, err := json.Marshal(map[string]string{"method": "isolated-postimage-v1", "output": output.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.PreserveQualityMutationOutput(f.admission, output, string(provenance)); err != nil {
		t.Fatal(err)
	}
}

func (f *qualityIntentFixture) qualityMutationRecordCount(t *testing.T) int {
	t.Helper()
	record, found, err := f.store.qualityMutationRecordForLease(f.admission.Lease.LeaseID)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		return 0
	}
	if record.Command != QualityMutationCommand {
		t.Fatalf("quality mutation record command = %q", record.Command)
	}
	return 1
}

func TestQualityMutationIntentSettlesCrashBeforeRecord(t *testing.T) {
	f := newQualityIntentFixture(t)
	f.stageQualityIntent(t)
	output := f.applyQualityOutput(t)
	f.preserveQualityOutput(t, output)

	if _, err := f.store.AdmitMutation(f.authority(), f.workspace, output); err == nil {
		t.Fatal("post-crash workspace must not match the live lease before settlement")
	}
	admission, err := f.store.AdmitMutationSettlingQualityIntent(f.authority(), f.workspace, output)
	if err != nil {
		t.Fatalf("settlement failed: %v", err)
	}
	if admission.Snapshot.ID != output.ID {
		t.Fatalf("settled admission snapshot = %s want output %s", admission.Snapshot.ID, output.ID)
	}
	head, err := f.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if head.ControllerGeneration != f.admission.Head.ControllerGeneration+1 {
		t.Fatalf("settlement advanced the generation to %d, want exactly %d", head.ControllerGeneration, f.admission.Head.ControllerGeneration+1)
	}
	if f.qualityMutationRecordCount(t) != 1 {
		t.Fatal("settlement must record the quality mutation exactly once")
	}
	if intent, err := f.store.loadQualityMutationIntent(); err != nil || intent != nil {
		t.Fatalf("settled intent must be cleared: %#v err=%v", intent, err)
	}
	if _, err := f.store.AdmitMutation(f.currentAuthority(t), f.workspace, output); err != nil {
		t.Fatalf("subsequent admission must accept the settled postimage: %v", err)
	}
}

func TestQualityMutationIntentCompletesPartiallyAppliedRecord(t *testing.T) {
	f := newQualityIntentFixture(t)
	f.stageQualityIntent(t)
	output := f.applyQualityOutput(t)
	f.preserveQualityOutput(t, output)

	mutationID, err := state.NewUUID()
	if err != nil {
		t.Fatal(err)
	}
	targetLeaseID, err := state.NewUUID()
	if err != nil {
		t.Fatal(err)
	}
	partial := MutationRecord{
		SchemaVersion:    controllerSchemaVersion,
		MutationID:       mutationID,
		AttemptID:        f.admission.Attempt.AttemptID,
		SourceLeaseID:    f.admission.Lease.LeaseID,
		TargetLeaseID:    targetLeaseID,
		SourceGeneration: f.admission.Head.ControllerGeneration,
		TargetGeneration: f.admission.Head.ControllerGeneration + 1,
		Command:          QualityMutationCommand,
		Outcome:          "success",
		Before:           f.before,
		After:            output,
	}
	data, err := json.Marshal(partial)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.store.mutationPath(mutationID), data, 0o600); err != nil {
		t.Fatal(err)
	}

	admission, err := f.store.AdmitMutationSettlingQualityIntent(f.authority(), f.workspace, output)
	if err != nil {
		t.Fatalf("partially applied settlement failed: %v", err)
	}
	if admission.Snapshot.ID != output.ID {
		t.Fatalf("settled admission snapshot = %s want output %s", admission.Snapshot.ID, output.ID)
	}
	if f.qualityMutationRecordCount(t) != 1 {
		t.Fatal("settlement must not duplicate the partially applied record")
	}
	head, err := f.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if head.ControllerGeneration != partial.TargetGeneration || head.LiveLeaseID != partial.TargetLeaseID {
		t.Fatalf("partial completion advanced head to generation=%d lease=%s, want %d/%s",
			head.ControllerGeneration, head.LiveLeaseID, partial.TargetGeneration, partial.TargetLeaseID)
	}
}

func TestQualityMutationIntentRejectsMissingProvenance(t *testing.T) {
	f := newQualityIntentFixture(t)
	f.stageQualityIntent(t)
	output := f.applyQualityOutput(t)

	_, err := f.store.AdmitMutationSettlingQualityIntent(f.authority(), f.workspace, output)
	if err == nil || !strings.Contains(err.Error(), "lacks preserved provenance") {
		t.Fatalf("unproven settlement must be rejected: %v", err)
	}
	head, headErr := f.store.LoadHead()
	if headErr != nil {
		t.Fatal(headErr)
	}
	if head.ControllerGeneration != f.admission.Head.ControllerGeneration {
		t.Fatal("rejected settlement must not advance the controller")
	}
}

func TestQualityMutationIntentRejectsSnapshotOutsideProvenBoundaries(t *testing.T) {
	f := newQualityIntentFixture(t)
	f.stageQualityIntent(t)
	output := f.applyQualityOutput(t)
	f.preserveQualityOutput(t, output)

	if err := os.WriteFile(filepath.Join(f.repo, "fixture.go"), []byte("package fixture\n\nvar concurrent = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	concurrent, err := CaptureWorkspaceSnapshot(f.repo)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.store.AdmitMutationSettlingQualityIntent(f.authority(), f.workspace, concurrent)
	if err == nil || !strings.Contains(err.Error(), "neither the staged quality mutation input") {
		t.Fatalf("concurrent snapshot must be rejected: %v", err)
	}
}

func TestQualityMutationIntentKeepsLeaseWhenPostimageNotApplied(t *testing.T) {
	f := newQualityIntentFixture(t)
	f.stageQualityIntent(t)

	if _, err := f.store.AdmitMutation(f.authority(), f.workspace, f.before); err != nil {
		t.Fatalf("unapplied intent must not disturb the live lease: %v", err)
	}
	if f.qualityMutationRecordCount(t) != 0 {
		t.Fatal("no source mutation may be recorded while the postimage is unapplied")
	}
	head, err := f.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if head.ControllerGeneration != f.admission.Head.ControllerGeneration {
		t.Fatal("unapplied intent must not advance the controller")
	}
}

func TestQualityMutationIntentRejectsDifferentWorkspaceIdentity(t *testing.T) {
	f := newQualityIntentFixture(t)
	f.stageQualityIntent(t)
	output := f.applyQualityOutput(t)
	f.preserveQualityOutput(t, output)

	otherWorkspace, err := ResolveWorkspaceIdentity(f.other, f.store.Identity())
	if err != nil {
		t.Fatal(err)
	}
	if otherWorkspace.ID == f.workspace.ID {
		t.Skip("linked worktree pair shares the workspace identity")
	}
	_, err = f.store.AdmitMutationSettlingQualityIntent(f.authority(), otherWorkspace, output)
	if err == nil || !strings.Contains(err.Error(), "different workspace") {
		t.Fatalf("foreign workspace settlement must be rejected: %v", err)
	}
}

func TestQualityMutationSettlementRejectsInvalidCallerClaim(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*MutationAuthority)
	}{
		{"stale-generation", func(a *MutationAuthority) { a.ControllerGeneration += 5 }},
		{"foreign-lease", func(a *MutationAuthority) { a.LeaseID = "foreign-lease" }},
		{"foreign-attempt", func(a *MutationAuthority) { a.AttemptID = "foreign-attempt" }},
		{"foreign-task", func(a *MutationAuthority) { a.SemanticTaskRef = SemanticTaskRef{} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newQualityIntentFixture(t)
			f.stageQualityIntent(t)
			output := f.applyQualityOutput(t)
			f.preserveQualityOutput(t, output)

			claim := f.authority()
			tc.mutate(&claim)
			if _, err := f.store.AdmitMutation(claim, f.workspace, output); err == nil {
				t.Fatal("invalid claim must not admit directly")
			}
			if _, err := f.store.AdmitMutationSettlingQualityIntent(claim, f.workspace, output); err == nil {
				t.Fatal("settlement must not accept an invalid caller claim")
			}
			if _, err := f.store.AdmitMutationOrFailClosed(claim, f.workspace, output); err == nil {
				t.Fatal("fail-closed settlement must not accept an invalid caller claim")
			}

			head, err := f.store.LoadHead()
			if err != nil {
				t.Fatal(err)
			}
			if head.ControllerGeneration != f.admission.Head.ControllerGeneration || head.LiveLeaseID != f.admission.Lease.LeaseID {
				t.Fatalf("rejected claim must not move the controller: %#v", head)
			}
			intent, err := f.store.loadQualityMutationIntent()
			if err != nil || intent == nil {
				t.Fatalf("rejected claim must preserve the staged intent: %#v err=%v", intent, err)
			}
			if f.qualityMutationRecordCount(t) != 0 {
				t.Fatal("rejected claim must not complete or create a mutation record")
			}
			if _, err := f.store.AdmitMutationSettlingQualityIntent(f.authority(), f.workspace, output); err != nil {
				t.Fatalf("legitimate settlement after the rejected claim must still succeed: %v", err)
			}
		})
	}
}

func TestStageQualityMutationDropsSettledLeftoverIntent(t *testing.T) {
	f := newQualityIntentFixture(t)
	f.stageQualityIntent(t)
	output := f.applyQualityOutput(t)
	f.preserveQualityOutput(t, output)

	if _, err := f.store.RecordAdmittedMutation(f.admission, QualityMutationCommand, "success", output); err != nil {
		t.Fatal(err)
	}
	next, err := f.store.AdmitMutation(f.currentAuthority(t), f.workspace, output)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.StageQualityMutation(next); err != nil {
		t.Fatalf("a settled leftover intent must not block the next quality gate: %v", err)
	}
	intent, err := f.store.loadQualityMutationIntent()
	if err != nil {
		t.Fatal(err)
	}
	if intent == nil || !sameQualityIntentAdmission(*intent, next) {
		t.Fatalf("fresh intent must replace the settled leftover: %#v", intent)
	}
}
