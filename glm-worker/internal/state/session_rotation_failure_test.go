package state

import (
	"encoding/json"
	"testing"
)

func seedClaimedSessionRotation(t *testing.T) (*StateStore, string, *SessionRotationMarker, SessionRotationClaim) {
	t.Helper()
	st := &StateStore{dir: t.TempDir()}
	parentThread := "01a0463c-d477-7410-9efd-cb34ff2e0b0e"
	if err := st.commitSessionRotation(&SessionRotationEvaluation{
		ParentThreadID: parentThread,
		TaskID:         "12345678-aaaa-bbbb-cccc-dddddddddddd",
		Terminal:       SessionRotationTerminalAccept,
		Decision: SessionRotationDecision{
			Required: true,
			Reason:   SessionRotationReasonCompaction,
		},
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
	return st, parentThread, marker, claim
}

func sessionRotationFailedCreationResult(parentThreadID, directiveID string, claim SessionRotationClaim) SessionRotationCreationResult {
	return SessionRotationCreationResult{
		Version:        SessionRotationCreationResultVersion,
		Outcome:        SessionRotationCreationOutcomeFailed,
		ParentThreadID: parentThreadID,
		DirectiveID:    directiveID,
		ClaimID:        claim.ClaimID,
		TargetTaskID:   claim.TargetTaskID,
	}
}

func TestReleaseSessionRotationClaimRequiresBoundConfirmedFailure(t *testing.T) {
	mutations := []struct {
		name   string
		mutate func(*SessionRotationCreationResult)
	}{
		{"unknown outcome", func(r *SessionRotationCreationResult) { r.Outcome = "unknown" }},
		{"unsupported version", func(r *SessionRotationCreationResult) { r.Version++ }},
		{"wrong parent", func(r *SessionRotationCreationResult) { r.ParentThreadID = "01a0244a-4ee4-7e71-b2e1-dec3bdda2120" }},
		{"wrong directive", func(r *SessionRotationCreationResult) { r.DirectiveID = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee" }},
		{"wrong claim", func(r *SessionRotationCreationResult) { r.ClaimID = "bbbbbbbb-cccc-4ddd-8eee-ffffffffffff" }},
		{"wrong target", func(r *SessionRotationCreationResult) { r.TargetTaskID = "cccccccc-dddd-4eee-8fff-aaaaaaaaaaaa" }},
	}
	for _, tc := range mutations {
		t.Run(tc.name, func(t *testing.T) {
			st, parentThread, marker, claim := seedClaimedSessionRotation(t)
			result := sessionRotationFailedCreationResult(parentThread, marker.Directive.DirectiveID, claim)
			tc.mutate(&result)
			if err := st.ReleaseSessionRotationClaim(parentThread, marker.Directive.DirectiveID, claim.ClaimID, result); err == nil {
				t.Fatal("creation failure proofなしでclaimがreleaseされました")
			}
			current, err := st.LoadSessionRotationMarker(parentThread)
			if err != nil {
				t.Fatal(err)
			}
			if current.State != SessionRotationStateClaimed || current.Claim == nil || current.Claim.ClaimID != claim.ClaimID || current.LastCreationOutcome != nil {
				t.Fatalf("拒否後のmarker = %#v", current)
			}
		})
	}
}

func TestReleaseSessionRotationClaimRejectsWrongCommandIdentity(t *testing.T) {
	st, parentThread, marker, claim := seedClaimedSessionRotation(t)
	result := sessionRotationFailedCreationResult(parentThread, marker.Directive.DirectiveID, claim)
	wrongDirective := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	wrongClaim := "bbbbbbbb-cccc-4ddd-8eee-ffffffffffff"
	wrongParent := "01a0244a-4ee4-7e71-b2e1-dec3bdda2120"
	for _, call := range []func() error{
		func() error {
			return st.ReleaseSessionRotationClaim(parentThread, wrongDirective, claim.ClaimID, result)
		},
		func() error {
			return st.ReleaseSessionRotationClaim(parentThread, marker.Directive.DirectiveID, wrongClaim, result)
		},
		func() error {
			return st.ReleaseSessionRotationClaim(wrongParent, marker.Directive.DirectiveID, claim.ClaimID, result)
		},
	} {
		if err := call(); err == nil {
			t.Fatal("wrong command identityがreleaseを通過しました")
		}
	}
	current, err := st.LoadSessionRotationMarker(parentThread)
	if err != nil || current.State != SessionRotationStateClaimed || current.Claim == nil || current.Claim.ClaimID != claim.ClaimID {
		t.Fatalf("wrong identity拒否後のmarker = %#v err=%v", current, err)
	}
}

func TestReleaseSessionRotationClaimPersistsFailureAndRejectsStaleRetry(t *testing.T) {
	st, parentThread, marker, first := seedClaimedSessionRotation(t)
	firstResult := sessionRotationFailedCreationResult(parentThread, marker.Directive.DirectiveID, first)
	if err := st.ReleaseSessionRotationClaim(parentThread, marker.Directive.DirectiveID, first.ClaimID, firstResult); err != nil {
		t.Fatal(err)
	}
	pending, err := st.LoadSessionRotationMarker(parentThread)
	if err != nil {
		t.Fatal(err)
	}
	if pending.State != SessionRotationStatePending || pending.Claim != nil || pending.LastCreationOutcome == nil {
		t.Fatalf("confirmed failure後のmarker = %#v", pending)
	}
	if pending.LastCreationOutcome.ClaimID != first.ClaimID || pending.LastCreationOutcome.TargetTaskID != first.TargetTaskID || pending.LastCreationOutcome.Outcome != SessionRotationCreationOutcomeFailed {
		t.Fatalf("persisted creation outcome = %#v", pending.LastCreationOutcome)
	}

	retry, err := st.ClaimSessionRotation(parentThread, marker.Directive.DirectiveID)
	if err != nil {
		t.Fatal(err)
	}
	if retry.ClaimID == first.ClaimID || retry.TargetTaskID == first.TargetTaskID {
		t.Fatalf("retry claimが更新されていません: first=%#v retry=%#v", first, retry)
	}
	if err := st.ReleaseSessionRotationClaim(parentThread, marker.Directive.DirectiveID, retry.ClaimID, firstResult); err == nil {
		t.Fatal("stale failure proofが新しいclaimをreleaseしました")
	}
	claimed, err := st.LoadSessionRotationMarker(parentThread)
	if err != nil || claimed.State != SessionRotationStateClaimed || claimed.Claim == nil || claimed.Claim.ClaimID != retry.ClaimID {
		t.Fatalf("stale proof拒否後のmarker = %#v err=%v", claimed, err)
	}

	retryResult := sessionRotationFailedCreationResult(parentThread, marker.Directive.DirectiveID, retry)
	if err := st.ReleaseSessionRotationClaim(parentThread, marker.Directive.DirectiveID, retry.ClaimID, retryResult); err != nil {
		t.Fatal(err)
	}
	pending, err = st.LoadSessionRotationMarker(parentThread)
	if err != nil || pending.LastCreationOutcome == nil || pending.LastCreationOutcome.ClaimID != retry.ClaimID {
		t.Fatalf("retry failure outcome = %#v err=%v", pending, err)
	}
}

func TestReleaseSessionRotationClaimRejectsBoundAndIssuedState(t *testing.T) {
	t.Run("bound", func(t *testing.T) {
		st, parentThread, marker, claim := seedClaimedSessionRotation(t)
		newThread := "01a0244a-4ee4-7e71-b2e1-dec3bdda2120"
		if err := st.BindSessionRotationClaim(parentThread, marker.Directive.DirectiveID, claim.ClaimID, newThread); err != nil {
			t.Fatal(err)
		}
		result := sessionRotationFailedCreationResult(parentThread, marker.Directive.DirectiveID, claim)
		if err := st.ReleaseSessionRotationClaim(parentThread, marker.Directive.DirectiveID, claim.ClaimID, result); err == nil {
			t.Fatal("bound claimがreleaseされました")
		}
	})

	t.Run("issued", func(t *testing.T) {
		st, parentThread, marker, claim := seedClaimedSessionRotation(t)
		newThread := "01a0244a-4ee4-7e71-b2e1-dec3bdda2120"
		if err := st.BindSessionRotationClaim(parentThread, marker.Directive.DirectiveID, claim.ClaimID, newThread); err != nil {
			t.Fatal(err)
		}
		if _, err := st.StartSessionRotationTask(newThread, claim.ClaimID); err != nil {
			t.Fatal(err)
		}
		if err := st.SetParentCodexIdentity(newThread, newThread, nil); err != nil {
			t.Fatal(err)
		}
		if err := st.AcknowledgeSessionRotationClaim(claim.ClaimID, newThread); err != nil {
			t.Fatal(err)
		}
		result := sessionRotationFailedCreationResult(parentThread, marker.Directive.DirectiveID, claim)
		if err := st.ReleaseSessionRotationClaim(parentThread, marker.Directive.DirectiveID, claim.ClaimID, result); err == nil {
			t.Fatal("issued claimがreleaseされました")
		}
	})
}

func TestSessionRotationMarkerRejectsOldVersions(t *testing.T) {
	_, _, marker, _ := seedClaimedSessionRotation(t)
	marker.LastCreationOutcome = nil
	for _, version := range []int{1, 2} {
		marker.Version = version
		data, err := json.Marshal(marker)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := decodeSessionRotationMarker(data); err == nil {
			t.Fatalf("version %d marker was accepted", version)
		}
	}
}
