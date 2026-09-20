package state

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
)

func TestReopenAcceptedParentCompletionAllowsFreshFindingOnNewAcceptedPublication(t *testing.T) {
	st := newAcceptedParentCompletionStore(t)
	firstCandidate := saveReopenPublicationState(t, st)
	firstFinding := recordReopenFinding(t, st)
	if err := st.ReopenAcceptedParentCompletion(); err != nil {
		t.Fatal(err)
	}

	reacceptReopenedParentCompletion(t, st)
	saveSecondReopenPublicationState(t, st, firstCandidate.TaskID)
	secondFinding := recordReopenFinding(t, st)
	if secondFinding.FindingID == firstFinding.FindingID {
		t.Fatalf("second finding reused first identity: %s", secondFinding.FindingID)
	}
	if secondFinding.CandidateCommitOID == firstFinding.CandidateCommitOID || secondFinding.CandidateSnapshotID == firstFinding.CandidateSnapshotID {
		t.Fatalf("second finding reused first publication identity: first=%#v second=%#v", firstFinding, secondFinding)
	}
	plan, err := st.ParentActionPlan()
	if err != nil {
		t.Fatal(err)
	}
	if plan.RequiredAction != ParentActionReopen || !plan.Allows(ParentActionReopen) || plan.Allows(ParentActionComplete) {
		t.Fatalf("second finding plan = %#v", plan)
	}
	if err := st.ReopenAcceptedParentCompletion(); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != TaskStatusWaitingSolReview {
		t.Fatalf("second reopen status = %s", st.TaskStatus())
	}
	if _, err := st.LoadPublicationCandidate(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("second reopen kept candidate: %v", err)
	}
	if _, err := st.LoadRuntimeInstallEvidence(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("second reopen kept install evidence: %v", err)
	}
	if _, err := st.LoadPublicationInvalidatingFinding(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("second reopen kept invalidating finding: %v", err)
	}
	lineage, err := st.LoadPublicationReopenLineage()
	if err != nil {
		t.Fatal(err)
	}
	if lineage.CommitOID != firstCandidate.CommitOID || lineage.BaseHead != firstCandidate.BaseHead {
		t.Fatalf("repeated reopen replaced first lineage anchor: %#v", lineage)
	}
}

func reacceptReopenedParentCompletion(t *testing.T, st *StateStore) {
	t.Helper()
	if err := st.SetTaskStatus(TaskStatusComplete); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordSolResult(packet.Result{Status: packet.StatusPass, Risk: packet.RiskHigh}, ParentReviewProducer{}); err != nil {
		t.Fatal(err)
	}
	accepted, err := st.AcceptParentReview()
	if err != nil || !accepted {
		t.Fatalf("second accept = %v err=%v", accepted, err)
	}
}

func saveSecondReopenPublicationState(t *testing.T, st *StateStore, taskID string) PublicationCandidate {
	t.Helper()
	head := strings.Repeat("2", 40)
	snapshot := SnapshotDigest{
		Head:           head,
		IndexDigest:    strings.Repeat("f", 64),
		WorktreeDigest: strings.Repeat("1", 64),
	}
	candidate := PublicationCandidate{
		Version:       publicationCandidateVersion,
		TaskID:        taskID,
		BaseHead:      head,
		CommitOID:     head,
		TreeOID:       strings.Repeat("3", 40),
		MessageDigest: strings.Repeat("4", 64),
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
		SourceDigest:      strings.Repeat("5", 64),
		InstalledRevision: head,
		SmokeResult:       ValidationResultPass,
	}); err != nil {
		t.Fatal(err)
	}
	return candidate
}
