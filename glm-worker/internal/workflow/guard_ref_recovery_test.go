package workflow

import (
	"errors"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestRefMutationGuardStopPersistsEvidenceAndRequiresExactRepair(t *testing.T) {
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	beforeDigest, err := runner.CaptureGitAuthorityRefDigest(repo)
	if err != nil {
		t.Fatal(err)
	}
	refErr := &runner.GitAuthorityGuardError{
		Stage:           "after-call-mutation",
		Mutations:       []string{"refs"},
		RefBeforeDigest: beforeDigest,
		RefAfterDigest:  "different-after-digest",
		RefChanges: []runner.GitRefChange{{
			Name:  "refs/heads/bypass",
			After: &runner.GitRefState{Name: "refs/heads/bypass", ObjectID: "after-object"},
		}},
	}
	stopRunner := &scriptedRunner{steps: []runnerStep{{structured: implementedPacket("done"), runErr: refErr}}}
	stopWorkflow := newGitWorkflowT(t, st, stopRunner, repo)
	_, err = stopWorkflow.runModel(workerCheckpoint())
	var stopped *GuardRecoverableError
	if !errors.As(err, &stopped) {
		t.Fatalf("refs-only mutation should enter guard recovery: %v", err)
	}
	checkpoint := retentionCheckpoint(t, st)
	if checkpoint.GuardRefBeforeDigest != beforeDigest || checkpoint.GuardRefAfterDigest != "different-after-digest" || len(checkpoint.GuardRefChanges) != 1 {
		t.Fatalf("ref evidence not retained: %#v", checkpoint)
	}
	if checkpoint.GuardRefStopDigest == "" {
		t.Fatal("stop-time ref authority digest was not retained")
	}
	if checkpoint.CompletedResult == nil {
		t.Fatal("completed worker result should remain reusable after exact ref repair")
	}

	runRetentionGit(t, repo, "branch", "bypass")
	blockedRunner := &scriptedRunner{}
	blockedWorkflow := newGitWorkflowT(t, st, blockedRunner, repo)
	err = blockedWorkflow.ExecuteResume()
	var workerErr *WorkerError
	if !errors.As(err, &workerErr) || !strings.Contains(workerErr.Message, "refs are not restored") {
		t.Fatalf("unrepaired refs must fail closed: %v", err)
	}
	if len(blockedRunner.prompts) != 0 {
		t.Fatalf("unrepaired refs dispatched model calls: %d", len(blockedRunner.prompts))
	}
	if st.TaskStatus() != state.TaskStatusGuardRecoverable {
		t.Fatalf("status after rejected resume = %s", st.TaskStatus())
	}

	runRetentionGit(t, repo, "branch", "-D", "bypass")
	repairedDigest, err := runner.CaptureGitAuthorityRefDigest(repo)
	if err != nil {
		t.Fatal(err)
	}
	if repairedDigest != beforeDigest {
		t.Fatalf("fixture did not restore exact refs: got %s want %s", repairedDigest, beforeDigest)
	}
	resumeRunner := &scriptedRunner{steps: []runnerStep{{structured: passPacket()}}}
	resumeWorkflow := newGitWorkflowT(t, st, resumeRunner, repo)
	resumeWorkflow.collectChangedPaths = func(string, string) ([]string, error) { return nil, nil }
	if err := resumeWorkflow.ExecuteResume(); err != nil {
		t.Fatalf("repaired refs should resume same task: %v", err)
	}
	if st.TaskStatus() != state.TaskStatusComplete {
		t.Fatalf("status = %s want complete", st.TaskStatus())
	}
	if len(resumeRunner.phases) != 1 || resumeRunner.phases[0] != "reviewer-1" {
		t.Fatalf("completed worker call was duplicated: phases=%v", resumeRunner.phases)
	}
}

func TestRefMutationGuardRecoveryAcceptsOnlyUntruncatedVolatileEvidence(t *testing.T) {
	volatile := []state.GuardRefChange{
		{Name: "refs/codex/turn-diffs/fixture"},
		{Name: "refs/codex/snapshots/fixture"},
	}
	if !guardRefChangesOnlyVolatile(volatile) {
		t.Fatal("volatile Desktop refs were not recognized")
	}
	nonvolatile := append(append([]state.GuardRefChange(nil), volatile...), state.GuardRefChange{Name: "refs/codex/authority/fixture"})
	if guardRefChangesOnlyVolatile(nonvolatile) {
		t.Fatal("nonvolatile Codex ref was excluded from recovery guard")
	}
}

func TestRefMutationGuardRecoveryResumesLegacyVolatileFailure(t *testing.T) {
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	refErr := legacyVolatileRefError()
	stopRunner := &scriptedRunner{steps: []runnerStep{{structured: implementedPacket("done"), runErr: refErr}}}
	stopWorkflow := newGitWorkflowT(t, st, stopRunner, repo)
	_, err := stopWorkflow.runModel(workerCheckpoint())
	var stopped *GuardRecoverableError
	if !errors.As(err, &stopped) {
		t.Fatalf("volatile legacy failure should enter guard recovery: %v", err)
	}
	checkpoint := retentionCheckpoint(t, st)
	if checkpoint.GuardRefStopDigest == "" {
		t.Fatal("legacy volatile failure did not retain stop-time authority refs")
	}

	resumeRunner := &scriptedRunner{steps: []runnerStep{{structured: passPacket()}}}
	resumeWorkflow := newGitWorkflowT(t, st, resumeRunner, repo)
	resumeWorkflow.collectChangedPaths = func(string, string) ([]string, error) { return nil, nil }
	if err := resumeWorkflow.ExecuteResume(); err != nil {
		t.Fatalf("volatile legacy failure should resume without ref restoration: %v", err)
	}
	if len(resumeRunner.phases) != 1 || resumeRunner.phases[0] != "reviewer-1" {
		t.Fatalf("resume phases = %v", resumeRunner.phases)
	}
}

func TestRefMutationGuardRecoveryRejectsPostStopNonvolatileMutationForLegacyVolatileFailure(t *testing.T) {
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	stopRunner := &scriptedRunner{steps: []runnerStep{{structured: implementedPacket("done"), runErr: legacyVolatileRefError()}}}
	stopWorkflow := newGitWorkflowT(t, st, stopRunner, repo)
	_, err := stopWorkflow.runModel(workerCheckpoint())
	var stopped *GuardRecoverableError
	if !errors.As(err, &stopped) {
		t.Fatalf("volatile legacy failure should enter guard recovery: %v", err)
	}

	runRetentionGit(t, repo, "branch", "post-stop-authority-change")
	blockedRunner := &scriptedRunner{}
	blockedWorkflow := newGitWorkflowT(t, st, blockedRunner, repo)
	err = blockedWorkflow.ExecuteResume()
	var workerErr *WorkerError
	if !errors.As(err, &workerErr) || !strings.Contains(workerErr.Message, "refs changed after stop") {
		t.Fatalf("post-stop nonvolatile ref mutation must fail closed: %v", err)
	}
	if len(blockedRunner.prompts) != 0 {
		t.Fatalf("post-stop ref mutation dispatched model calls: %d", len(blockedRunner.prompts))
	}
	if st.TaskStatus() != state.TaskStatusGuardRecoverable {
		t.Fatalf("status after rejected resume = %s", st.TaskStatus())
	}
}

func legacyVolatileRefError() *runner.GitAuthorityGuardError {
	return &runner.GitAuthorityGuardError{
		Stage:           "after-call-mutation",
		Mutations:       []string{"refs"},
		RefBeforeDigest: "legacy-full-ref-before",
		RefAfterDigest:  "legacy-full-ref-after",
		RefChanges: []runner.GitRefChange{
			{Name: "refs/codex/turn-diffs/fixture"},
			{Name: "refs/codex/snapshots/fixture"},
		},
	}
}

func TestRefMutationGuardRecoveryFailsClosedOnTruncatedVolatileEvidence(t *testing.T) {
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	refErr := &runner.GitAuthorityGuardError{
		Stage:               "after-call-mutation",
		Mutations:           []string{"refs"},
		RefBeforeDigest:     "legacy-full-ref-before",
		RefAfterDigest:      "legacy-full-ref-after",
		RefChangesTruncated: true,
		RefChanges: []runner.GitRefChange{
			{Name: "refs/codex/turn-diffs/fixture"},
		},
	}
	stopRunner := &scriptedRunner{steps: []runnerStep{{structured: implementedPacket("done"), runErr: refErr}}}
	stopWorkflow := newGitWorkflowT(t, st, stopRunner, repo)
	_, err := stopWorkflow.runModel(workerCheckpoint())
	var stopped *GuardRecoverableError
	if !errors.As(err, &stopped) {
		t.Fatalf("truncated volatile failure should enter guard recovery: %v", err)
	}

	blockedRunner := &scriptedRunner{}
	blockedWorkflow := newGitWorkflowT(t, st, blockedRunner, repo)
	err = blockedWorkflow.ExecuteResume()
	var workerErr *WorkerError
	if !errors.As(err, &workerErr) || !strings.Contains(workerErr.Message, "refs are not restored") {
		t.Fatalf("truncated evidence must fail closed without volatile free-pass: %v", err)
	}
	if len(blockedRunner.prompts) != 0 {
		t.Fatalf("truncated evidence dispatched model calls: %d", len(blockedRunner.prompts))
	}
	if st.TaskStatus() != state.TaskStatusGuardRecoverable {
		t.Fatalf("status after truncated evidence resume = %s", st.TaskStatus())
	}
}
