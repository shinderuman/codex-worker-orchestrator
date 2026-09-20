package state

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
)

func newAcceptedParentCompletionStore(t *testing.T) *StateStore {
	t.Helper()
	st := newParentActionTestStore(t)
	if err := st.SaveCurrentTaskAuthority("IMPLEMENTATION_TASKS/active.md", []byte("# active\n")); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(TaskStatusComplete); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordSolResult(packet.Result{Status: packet.StatusPass, Risk: packet.RiskHigh}, ParentReviewProducer{}); err != nil {
		t.Fatal(err)
	}
	accepted, err := st.AcceptParentReview()
	if err != nil || !accepted {
		t.Fatalf("accept = %v err=%v", accepted, err)
	}
	return st
}

func saveReopenPublicationState(t *testing.T, st *StateStore) PublicationCandidate {
	t.Helper()
	taskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	head := strings.Repeat("1", 40)
	snapshot := SnapshotDigest{Head: head, IndexDigest: strings.Repeat("a", 64), WorktreeDigest: strings.Repeat("b", 64)}
	candidate := PublicationCandidate{
		Version:       publicationCandidateVersion,
		TaskID:        taskID,
		BaseHead:      head,
		CommitOID:     head,
		TreeOID:       strings.Repeat("c", 40),
		MessageDigest: strings.Repeat("d", 64),
		Snapshot:      snapshot,
		SnapshotID:    ValidationSnapshotID(snapshot.Head, snapshot.IndexDigest, snapshot.WorktreeDigest),
		PreparedAt:    time.Now().UTC(),
	}
	if err := st.SavePublicationCandidate(candidate); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveRuntimeInstallEvidence(RuntimeInstallEvidence{
		Version:           runtimeInstallEvidenceVersion,
		TaskID:            taskID,
		Head:              head,
		SourceDigest:      strings.Repeat("e", 64),
		InstalledRevision: head,
		SmokeResult:       ValidationResultPass,
	}); err != nil {
		t.Fatal(err)
	}
	return candidate
}

func recordReopenFinding(t *testing.T, st *StateStore) PublicationInvalidatingFinding {
	t.Helper()
	finding, err := st.RecordPublicationInvalidatingFinding(
		ParentOriginCodexReview,
		ParentCauseProductionWiring,
	)
	if err != nil {
		t.Fatal(err)
	}
	return finding
}

func TestReopenAcceptedParentCompletionReturnsTaskToFixLifecycle(t *testing.T) {
	st := newAcceptedParentCompletionStore(t)
	candidate := saveReopenPublicationState(t, st)
	plan, err := st.ParentActionPlan()
	if err != nil {
		t.Fatal(err)
	}
	if plan.RequiredAction != ParentActionComplete || !plan.Allows(ParentActionComplete) || plan.Allows(ParentActionReopen) {
		t.Fatalf("awaiting plan before finding = %#v", plan)
	}

	finding := recordReopenFinding(t, st)
	plan, err = st.ParentActionPlan()
	if err != nil {
		t.Fatal(err)
	}
	if plan.RequiredAction != ParentActionReopen || !plan.Allows(ParentActionReopen) || plan.Allows(ParentActionComplete) || plan.Allows(ParentActionInstall) {
		t.Fatalf("awaiting plan after finding = %#v", plan)
	}
	if finding.TaskID != candidate.TaskID || finding.CandidateCommitOID != candidate.CommitOID || finding.CandidateSnapshotID != candidate.SnapshotID || finding.Disposition != PublicationFindingCorrectnessDefect {
		t.Fatalf("finding = %#v candidate=%#v", finding, candidate)
	}

	if err := st.ReopenAcceptedParentCompletion(); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != TaskStatusWaitingSolReview {
		t.Fatalf("reopen status = %s", st.TaskStatus())
	}
	label, err := st.CurrentParentReviewLabel()
	if err != nil || label != string(packet.StatusNeedsSolReview) {
		t.Fatalf("reopened review label = %q err=%v", label, err)
	}
	completion, err := st.CurrentParentCompletionOutcome()
	if err != nil || completion != nil {
		t.Fatalf("reopened completion outcome = %#v err=%v", completion, err)
	}
	if _, err := st.LoadPublicationCandidate(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("reopened publication candidate = %v", err)
	}
	if _, err := st.LoadRuntimeInstallEvidence(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("reopened runtime install evidence = %v", err)
	}
	if _, err := st.LoadPublicationInvalidatingFinding(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("reopened invalidating finding = %v", err)
	}
	lineage, err := st.LoadPublicationReopenLineage()
	if err != nil {
		t.Fatal(err)
	}
	if lineage.TaskID != candidate.TaskID || lineage.TaskPath != "IMPLEMENTATION_TASKS/active.md" || lineage.BaseHead != candidate.BaseHead || lineage.CommitOID != candidate.CommitOID {
		t.Fatalf("reopen lineage = %#v candidate=%#v", lineage, candidate)
	}
	after, err := st.ParentActionPlan()
	if err != nil {
		t.Fatal(err)
	}
	if after.RequiredAction != ParentActionReview || !after.Allows(ParentActionFix) || !after.Allows(ParentActionPark) {
		t.Fatalf("reopened plan = %#v", after)
	}
	if after.Allows(ParentActionAccept) || after.Allows(ParentActionComplete) || after.Allows(ParentActionInstall) || after.Allows(ParentActionReopen) {
		t.Fatalf("reopened plan keeps terminal actions: %#v", after)
	}
}

func TestReopenAcceptedParentCompletionRollsBackLineageOnIntermediateFailure(t *testing.T) {
	st := newAcceptedParentCompletionStore(t)
	saveReopenPublicationState(t, st)
	findingBefore := recordReopenFinding(t, st)
	candidateBefore, err := st.LoadPublicationCandidate()
	if err != nil {
		t.Fatal(err)
	}
	evidenceBefore, err := st.LoadRuntimeInstallEvidence()
	if err != nil {
		t.Fatal(err)
	}
	evidencePath := st.Path(runtimeInstallEvidenceFile)
	original := removeStatePath
	removeStatePath = func(path string) error {
		if path == evidencePath {
			return errors.New("injected runtime install evidence remove failure")
		}
		return original(path)
	}
	t.Cleanup(func() { removeStatePath = original })

	err = st.ReopenAcceptedParentCompletion()
	if err == nil || !strings.Contains(err.Error(), "injected runtime install evidence remove failure") {
		t.Fatalf("intermediate reopen failure = %v", err)
	}
	if st.TaskStatus() != TaskStatusAwaitingParentCompletion {
		t.Fatalf("failed reopen mutated status: %s", st.TaskStatus())
	}
	candidateAfter, err := st.LoadPublicationCandidate()
	if err != nil || candidateAfter != candidateBefore {
		t.Fatalf("failed reopen did not restore publication candidate: %#v err=%v", candidateAfter, err)
	}
	evidenceAfter, err := st.LoadRuntimeInstallEvidence()
	if err != nil || evidenceAfter != evidenceBefore {
		t.Fatalf("failed reopen did not restore runtime install evidence: %#v err=%v", evidenceAfter, err)
	}
	findingAfter, err := st.LoadPublicationInvalidatingFinding()
	if err != nil || findingAfter != findingBefore {
		t.Fatalf("failed reopen did not restore invalidating finding: %#v err=%v", findingAfter, err)
	}
	if _, err := st.LoadPublicationReopenLineage(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed reopen left lineage anchor: %v", err)
	}
}

func TestCapturePublicationReopenLineagePreservesFirstAnchorAndRejectsWrongTask(t *testing.T) {
	st := newAcceptedParentCompletionStore(t)
	first := saveReopenPublicationState(t, st)
	if err := st.CapturePublicationReopenLineage(first); err != nil {
		t.Fatal(err)
	}

	second := first
	second.BaseHead = strings.Repeat("2", 40)
	second.CommitOID = strings.Repeat("3", 40)
	second.Snapshot.Head = second.BaseHead
	second.SnapshotID = ValidationSnapshotID(second.Snapshot.Head, second.Snapshot.IndexDigest, second.Snapshot.WorktreeDigest)
	if err := st.CapturePublicationReopenLineage(second); err != nil {
		t.Fatal(err)
	}
	lineage, err := st.LoadPublicationReopenLineage()
	if err != nil {
		t.Fatal(err)
	}
	if lineage.BaseHead != first.BaseHead || lineage.CommitOID != first.CommitOID {
		t.Fatalf("first lineage anchor was overwritten: %#v", lineage)
	}

	wrong := second
	wrong.TaskID = "00000000-0000-4000-8000-000000000001"
	if err := st.CapturePublicationReopenLineage(wrong); err == nil || !strings.Contains(err.Error(), "does not match current task") {
		t.Fatalf("wrong-task candidate lineage capture = %v", err)
	}
}

func TestReopenAcceptedParentCompletionFailsClosed(t *testing.T) {
	st := newParentActionTestStore(t)
	if err := st.SetTaskStatus(TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordSolResult(packet.Result{Status: packet.StatusNeedsSolReview, Risk: packet.RiskHigh}, ParentReviewProducer{}); err != nil {
		t.Fatal(err)
	}
	if err := st.ReopenAcceptedParentCompletion(); err == nil || !strings.Contains(err.Error(), "reopen requires") {
		t.Fatalf("reopen outside awaiting parent completion = %v", err)
	}

	awaitingWithoutAccept := newParentActionTestStore(t)
	if err := awaitingWithoutAccept.SetTaskStatus(TaskStatusAwaitingParentCompletion); err != nil {
		t.Fatal(err)
	}
	if err := awaitingWithoutAccept.ReopenAcceptedParentCompletion(); err == nil || !strings.Contains(err.Error(), "accepted parent completion outcome") {
		t.Fatalf("reopen without accepted outcome = %v", err)
	}

	noGoTerminal := newParentActionTestStore(t)
	if err := noGoTerminal.SetTaskStatus(TaskStatusAwaitingParentCompletion); err != nil {
		t.Fatal(err)
	}
	taskID, err := noGoTerminal.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	terminalState := ParentReviewState{
		Version:    parentReviewStateVersion,
		TaskID:     taskID,
		Completion: &ParentCompletionOutcome{Terminal: SessionRotationTerminalNoGo, Risk: string(packet.RiskLow)},
	}
	data, err := json.Marshal(terminalState)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(noGoTerminal.Path(parentReviewStateFile), append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := noGoTerminal.ReopenAcceptedParentCompletion(); err == nil || !strings.Contains(err.Error(), "accepted parent completion outcome") {
		t.Fatalf("reopen of no-go terminal = %v", err)
	}

	withoutFinding := newAcceptedParentCompletionStore(t)
	saveReopenPublicationState(t, withoutFinding)
	if err := withoutFinding.ReopenAcceptedParentCompletion(); err == nil || !strings.Contains(err.Error(), "durable invalidating correctness finding") {
		t.Fatalf("reopen without durable finding = %v", err)
	}
	if withoutFinding.TaskStatus() != TaskStatusAwaitingParentCompletion {
		t.Fatalf("failed reopen mutated status: %s", withoutFinding.TaskStatus())
	}
}
