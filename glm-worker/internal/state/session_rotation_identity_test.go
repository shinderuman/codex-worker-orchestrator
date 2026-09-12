package state

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestSessionRotationRejectsClaimantParentIdentityMismatch(t *testing.T) {
	st, parentThread, directiveID, claim := seedClaimedSessionRotationIdentityTest(t)
	marker, err := st.LoadSessionRotationMarker(parentThread)
	if err != nil {
		t.Fatal(err)
	}
	marker.Claim.ClaimantThreadID = "01a0244a-4ee4-7e71-b2e1-dec3bdda2120"

	if err := st.writeSessionRotationMarker(marker); err == nil {
		t.Fatal("claimant/parent thread identity mismatch was persisted")
	}
	writeRawSessionRotationMarkerIdentityTest(t, st, marker)
	if _, err := st.LoadSessionRotationMarker(parentThread); err == nil {
		t.Fatal("claimant/parent thread identity mismatch was decoded")
	}
	if _, err := st.ClaimSessionRotation(parentThread, directiveID); err == nil {
		t.Fatal("claimant/parent thread identity mismatch reached claim reuse")
	}
	if err := st.BindSessionRotationClaim(parentThread, directiveID, claim.ClaimID, "01a08268-2c7d-72a0-9aa1-605eed066ce9"); err == nil {
		t.Fatal("claimant/parent thread identity mismatch reached bind")
	}
}

func TestSessionRotationRejectsIssuedBoundIdentityMismatch(t *testing.T) {
	st, parentThread, directiveID, claim := seedClaimedSessionRotationIdentityTest(t)
	boundThread := "01a08268-2c7d-72a0-9aa1-605eed066ce9"
	if err := st.BindSessionRotationClaim(parentThread, directiveID, claim.ClaimID, boundThread); err != nil {
		t.Fatal(err)
	}
	marker, err := st.LoadSessionRotationMarker(parentThread)
	if err != nil {
		t.Fatal(err)
	}
	marker.State = SessionRotationStateIssued
	marker.Issued = &SessionRotationIssued{
		BoundThreadID: "01a0244a-4ee4-7e71-b2e1-dec3bdda2120",
		IssuedAt:      time.Now().UTC().Format(time.RFC3339Nano),
	}

	if err := st.writeSessionRotationMarker(marker); err == nil {
		t.Fatal("claim/issued bound thread identity mismatch was persisted")
	}
	writeRawSessionRotationMarkerIdentityTest(t, st, marker)
	if _, err := st.LoadSessionRotationMarker(parentThread); err == nil {
		t.Fatal("claim/issued bound thread identity mismatch was decoded")
	}
	if err := st.AcknowledgeSessionRotationClaim(claim.ClaimID, boundThread); err == nil {
		t.Fatal("claim/issued bound thread identity mismatch reached acknowledge")
	}
	if _, err := st.AdmitNewTaskRotation(boundThread, claim.ClaimID); err == nil {
		t.Fatal("claim/issued bound thread identity mismatch reached admission")
	}
	if _, err := st.StartSessionRotationTask(boundThread, claim.ClaimID); err == nil {
		t.Fatal("claim/issued bound thread identity mismatch reached start")
	}
}

func TestSessionRotationIdentityCoherencePreservesRetries(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	parentThread := "01a0463c-d477-7410-9efd-cb34ff2e0b0e"
	boundThread := "01a0244a-4ee4-7e71-b2e1-dec3bdda2120"
	if err := st.SetParentCodexIdentity(parentThread, parentThread, nil); err != nil {
		t.Fatal(err)
	}
	if err := st.commitSessionRotation(&SessionRotationEvaluation{
		ParentThreadID: parentThread,
		TaskID:         st.ReadOr("task.id", ""),
		Terminal:       SessionRotationTerminalAccept,
		Decision:       SessionRotationDecision{Required: true, Reason: SessionRotationReasonCompaction},
	}); err != nil {
		t.Fatal(err)
	}
	marker, err := st.LoadSessionRotationMarker(parentThread)
	if err != nil {
		t.Fatal(err)
	}
	claim, err := st.ClaimSessionRotation(parentThread, marker.Directive.DirectiveID)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := st.ClaimSessionRotation(parentThread, marker.Directive.DirectiveID)
	if err != nil || repeated.ClaimID != claim.ClaimID || repeated.TargetTaskID != claim.TargetTaskID {
		t.Fatalf("repeated claim = %#v err=%v", repeated, err)
	}
	if err := st.BindSessionRotationClaim(parentThread, marker.Directive.DirectiveID, claim.ClaimID, boundThread); err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartSessionRotationTask(boundThread, claim.ClaimID); err != nil {
		t.Fatal(err)
	}
	if err := st.SetParentCodexIdentity(boundThread, boundThread, nil); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveResumeCheckpoint(ResumeCheckpoint{Stage: ResumeStageWorker, Phase: "worker-new", Role: WorkerRole, Model: "opus"}); err != nil {
		t.Fatal(err)
	}
	if err := st.AcknowledgeSessionRotationClaim(claim.ClaimID, boundThread); err != nil {
		t.Fatal(err)
	}
	if err := st.AcknowledgeSessionRotationClaim(claim.ClaimID, boundThread); err != nil {
		t.Fatalf("issued acknowledgement retry failed: %v", err)
	}
	issued, err := st.LoadSessionRotationMarker(parentThread)
	if err != nil {
		t.Fatal(err)
	}
	if issued.Claim == nil || issued.Issued == nil || issued.Claim.ClaimantThreadID != parentThread || issued.Claim.BoundThreadID != boundThread || issued.Issued.BoundThreadID != boundThread {
		t.Fatalf("issued identity state = %#v", issued)
	}
	resume, err := st.AdmitNewTaskRotation(boundThread, claim.ClaimID)
	if err != nil || !resume {
		t.Fatalf("issued start retry = %v, %v", resume, err)
	}
}

func seedClaimedSessionRotationIdentityTest(t *testing.T) (*StateStore, string, string, SessionRotationClaim) {
	t.Helper()
	st := &StateStore{dir: t.TempDir()}
	parentThread := "01a0463c-d477-7410-9efd-cb34ff2e0b0e"
	if err := st.commitSessionRotation(&SessionRotationEvaluation{
		ParentThreadID: parentThread,
		TaskID:         "12345678-aaaa-bbbb-cccc-dddddddddddd",
		Terminal:       SessionRotationTerminalAccept,
		Decision:       SessionRotationDecision{Required: true, Reason: SessionRotationReasonCompaction},
	}); err != nil {
		t.Fatal(err)
	}
	marker, err := st.LoadSessionRotationMarker(parentThread)
	if err != nil {
		t.Fatal(err)
	}
	claim, err := st.ClaimSessionRotation(parentThread, marker.Directive.DirectiveID)
	if err != nil {
		t.Fatal(err)
	}
	return st, parentThread, marker.Directive.DirectiveID, claim
}

func writeRawSessionRotationMarkerIdentityTest(t *testing.T, st *StateStore, marker *SessionRotationMarker) {
	t.Helper()
	data, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(st.SessionRotationMarkerPath(marker.ParentThreadID), append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}
