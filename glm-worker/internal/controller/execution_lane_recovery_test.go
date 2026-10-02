package controller

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExecutionLaneSerialReplanCleanupRetryAndSamePathReuse(t *testing.T) {
	fixture := newFindingAcceptanceFixture(t, "IMPLEMENTATION_TASKS/deeper-blocker.md")
	episode := planSuspensionTestEpisode(t, fixture)
	root, err := fixture.store.SuspendExecution(fixture.source, episode.EpisodeID, episode.Revision)
	if err != nil {
		t.Fatal(err)
	}
	first, err := fixture.store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: root.Head.ControllerGeneration, EpisodeID: episode.EpisodeID, EpisodeRevision: episode.Revision})
	if err != nil {
		t.Fatal(err)
	}
	child := *first.Admission
	writeSuspensionTestFile(t, child.Workspace.Root, "child-work.txt", "preserved child work\n")
	changed, err := CaptureWorkspaceSnapshot(child.Workspace.Root)
	if err != nil {
		t.Fatal(err)
	}
	child, err = fixture.store.RecordAdmittedMutation(child, "child edit", "success", changed)
	if err != nil {
		t.Fatal(err)
	}
	project, err := fixture.store.LoadProjectSnapshot(child.Head.ProjectSnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	deeper := findProjectTask(project, "IMPLEMENTATION_TASKS/deeper-blocker.md")
	finding, err := fixture.store.ObserveFinding(child, FindingObservationInput{Producer: "reviewer", ProofClass: FindingProofUnverified, ProblemKey: "deeper-blocker"})
	if err != nil {
		t.Fatal(err)
	}
	planned, err := fixture.store.ResolveFindingWithProjectAuthority(finding.FindingID, FindingDecision{Kind: FindingDecisionIndependentBlocking, TargetTaskRef: &deeper, BlockingBoundary: "child requires deeper blocker"})
	if err != nil {
		t.Fatal(err)
	}
	suspended, err := fixture.store.SuspendExecution(child, planned.Episode.EpisodeID, planned.Episode.Revision)
	if err != nil {
		t.Fatal(err)
	}
	runControllerGit(t, fixture.source.Workspace.Root, "worktree", "lock", child.Workspace.Root)
	if _, err := fixture.store.CleanupExecution(CleanupExecutionInput{ExpectedGeneration: suspended.Head.ControllerGeneration, WorkspaceID: child.Workspace.ID, SealRef: *suspended.SealRef}); err == nil {
		t.Fatal("locked worktree cleanup succeeded")
	}
	pending, err := fixture.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if pending.PendingTransitionID == "" || pending.LiveLeaseID != "" {
		t.Fatal("cleanup failure discarded retry authority")
	}
	if _, err := fixture.store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: pending.ControllerGeneration, EpisodeID: planned.Episode.EpisodeID, EpisodeRevision: planned.Episode.Revision}); err == nil {
		t.Fatal("pending cleanup admitted another lane")
	}
	runControllerGit(t, fixture.source.Workspace.Root, "worktree", "unlock", child.Workspace.Root)
	cleaned, err := fixture.store.RecoverExecutionOperation(pending.PendingTransitionID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(child.Workspace.Root); !os.IsNotExist(err) {
		t.Fatalf("old lane survived cleanup: %v", err)
	}
	second, err := fixture.store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: cleaned.Head.ControllerGeneration, EpisodeID: planned.Episode.EpisodeID, EpisodeRevision: planned.Episode.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if second.Admission.Workspace.Root != child.Workspace.Root || second.Admission.Workspace.ID == child.Workspace.ID {
		t.Fatal("same-path reuse did not replace workspace nonce")
	}
	if _, err := fixture.store.AdmitMutation(child.MutationAuthority(), second.Admission.Workspace, second.Admission.Snapshot); err == nil {
		t.Fatal("stale lease regained authority at reused path")
	}
	for _, seal := range []EvidenceObjectRef{*root.SealRef, *suspended.SealRef} {
		if _, err := fixture.store.BuildAttemptEvidenceBundle(seal); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := fixture.store.LoadSuspension(fixture.source.Workspace.Root, suspended.Suspension.SnapshotID); err != nil {
		t.Fatal(err)
	}
}

func TestExecutionMaterializationRecoversEachDurableBoundary(t *testing.T) {
	for _, boundary := range []string{"prepared", "registered", "materialized", "committed", "head-committed"} {
		t.Run(boundary, func(t *testing.T) {
			fixture := newFindingAcceptanceFixture(t)
			episode := planSuspensionTestEpisode(t, fixture)
			suspended, err := fixture.store.SuspendExecution(fixture.source, episode.EpisodeID, episode.Revision)
			if err != nil {
				t.Fatal(err)
			}
			op, head, err := fixture.store.planExecutionMaterialization(MaterializeExecutionInput{ExpectedGeneration: suspended.Head.ControllerGeneration, EpisodeID: episode.EpisodeID, EpisodeRevision: episode.Revision})
			if err != nil {
				t.Fatal(err)
			}
			if err := fixture.store.prepareExecutionOperation(&op, head); err != nil {
				t.Fatal(err)
			}
			if boundary != "prepared" {
				if err := fixture.store.ensureExecutionLaneRegistration(*op.Workspace, op.Rebound.BaseOID); err != nil {
					t.Fatal(err)
				}
			}
			if boundary == "materialized" {
				if err := materializeExecutionTrees(*op.Workspace, *op.Rebound); err != nil {
					t.Fatal(err)
				}
			}
			if boundary == "committed" || boundary == "head-committed" {
				if err := fixture.store.applyExecutionMaterialization(op); err != nil {
					t.Fatal(err)
				}
			}
			if boundary == "head-committed" {
				phase, err := fixture.store.loadTransitionState(op.Transition.TransitionID)
				if err != nil {
					t.Fatal(err)
				}
				phase.Phase = TransitionPhasePrepared
				if err := fixture.store.writeTransitionState(phase); err != nil {
					t.Fatal(err)
				}
			}
			result, err := fixture.store.RecoverExecutionOperation(op.Transition.TransitionID)
			if err != nil {
				t.Fatal(err)
			}
			if result.Admission == nil || result.Admission.Workspace.ID != op.Workspace.ID || result.Head.PendingTransitionID != "" {
				t.Fatal("recovery did not commit the planned workspace/lease")
			}
		})
	}
}

func TestExecutionMaterializationUnknownPartialStatePreservesAuthority(t *testing.T) {
	fixture := newFindingAcceptanceFixture(t)
	episode := planSuspensionTestEpisode(t, fixture)
	suspended, err := fixture.store.SuspendExecution(fixture.source, episode.EpisodeID, episode.Revision)
	if err != nil {
		t.Fatal(err)
	}
	op, head, err := fixture.store.planExecutionMaterialization(MaterializeExecutionInput{ExpectedGeneration: suspended.Head.ControllerGeneration, EpisodeID: episode.EpisodeID, EpisodeRevision: episode.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.prepareExecutionOperation(&op, head); err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.ensureExecutionLaneRegistration(*op.Workspace, op.Rebound.BaseOID); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(op.Workspace.Root, "foreign.txt")
	if err := os.WriteFile(marker, []byte("foreign state"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.RecoverExecutionOperation(op.Transition.TransitionID); err == nil {
		t.Fatal("unknown partial state was adopted")
	}
	actual, err := fixture.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if actual.LiveLeaseID != "" || actual.PendingTransitionID != op.Transition.TransitionID {
		t.Fatal("failed materialization lost pending authority")
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "foreign state" {
		t.Fatal("unknown state was destroyed")
	}
}
