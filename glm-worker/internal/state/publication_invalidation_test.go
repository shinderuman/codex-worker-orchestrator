package state

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRecordPublicationInvalidatingFindingBindsCurrentCandidate(t *testing.T) {
	st := newAcceptedParentCompletionStore(t)
	candidate := saveReopenPublicationState(t, st)

	finding, err := st.RecordPublicationInvalidatingFinding(
		ParentOriginCodexReview,
		ParentCauseProductionWiring,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !ValidGeneratedUUID(finding.FindingID) {
		t.Fatalf("finding ID = %q", finding.FindingID)
	}
	if finding.TaskID != candidate.TaskID || finding.Disposition != PublicationFindingCorrectnessDefect || finding.CandidateCommitOID != candidate.CommitOID || finding.CandidateSnapshotID != candidate.SnapshotID {
		t.Fatalf("finding = %#v candidate=%#v", finding, candidate)
	}

	retry, err := st.RecordPublicationInvalidatingFinding(
		ParentOriginCodexReview,
		ParentCauseProductionWiring,
	)
	if err != nil {
		t.Fatal(err)
	}
	if retry != finding {
		t.Fatalf("idempotent retry changed finding: before=%#v after=%#v", finding, retry)
	}
}

func TestRecordPublicationInvalidatingFindingRequiresCurrentCandidate(t *testing.T) {
	st := newAcceptedParentCompletionStore(t)
	if _, err := st.RecordPublicationInvalidatingFinding(
		ParentOriginCodexReview,
		ParentCauseProductionWiring,
	); err == nil || !strings.Contains(err.Error(), "requires current publication candidate") {
		t.Fatalf("finding without candidate = %v", err)
	}
	if _, err := st.LoadPublicationInvalidatingFinding(); err == nil {
		t.Fatal("missing candidate left a durable finding")
	}
}

func TestCurrentPublicationInvalidatingFindingRejectsStaleCandidate(t *testing.T) {
	st := newAcceptedParentCompletionStore(t)
	candidate := saveReopenPublicationState(t, st)
	if _, err := st.RecordPublicationInvalidatingFinding(
		ParentOriginCodexReview,
		ParentCauseProductionWiring,
	); err != nil {
		t.Fatal(err)
	}

	staleTarget := candidate
	staleTarget.BaseHead = strings.Repeat("2", 40)
	staleTarget.CommitOID = strings.Repeat("3", 40)
	staleTarget.Snapshot.Head = staleTarget.BaseHead
	staleTarget.SnapshotID = ValidationSnapshotID(staleTarget.Snapshot.Head, staleTarget.Snapshot.IndexDigest, staleTarget.Snapshot.WorktreeDigest)
	if err := st.SavePublicationCandidate(staleTarget); err != nil {
		t.Fatal(err)
	}

	if _, err := st.CurrentPublicationInvalidatingFinding(); err == nil || !strings.Contains(err.Error(), "stale or unrelated") {
		t.Fatalf("stale finding = %v", err)
	}
	if _, err := st.ParentActionPlan(); err == nil || !strings.Contains(err.Error(), "publication invalidating finding state is invalid") {
		t.Fatalf("stale finding action plan = %v", err)
	}
}

func TestCurrentPublicationInvalidatingFindingRejectsWrongTask(t *testing.T) {
	st := newAcceptedParentCompletionStore(t)
	candidate := saveReopenPublicationState(t, st)
	finding := PublicationInvalidatingFinding{
		Version:             publicationInvalidatingFindingVersion,
		TaskID:              "00000000-0000-4000-8000-000000000001",
		FindingID:           "00000000-0000-4000-8000-000000000002",
		Disposition:         PublicationFindingCorrectnessDefect,
		CandidateCommitOID:  candidate.CommitOID,
		CandidateSnapshotID: candidate.SnapshotID,
		Origin:              ParentOriginCodexReview,
		Cause:               ParentCauseProductionWiring,
	}
	data, err := json.Marshal(finding)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Write(publicationInvalidatingFindingStateFile, string(data)); err != nil {
		t.Fatal(err)
	}

	if _, err := st.CurrentPublicationInvalidatingFinding(); err == nil || !strings.Contains(err.Error(), "does not match current task") {
		t.Fatalf("wrong-task finding = %v", err)
	}
}
