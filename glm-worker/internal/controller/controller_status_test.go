package controller

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestProjectStatusPreservesDiagnosticsWhenLiveAuthorityIsCorrupt(t *testing.T) {
	for _, kind := range []string{"attempt", "lease"} {
		t.Run(kind, func(t *testing.T) {
			harness := newLifecycleHarness(t)
			head, err := harness.store.LoadHead()
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(harness.store.dir, "attempts", head.LiveAttemptID+".json")
			if kind == "lease" {
				path = filepath.Join(harness.store.dir, "leases", head.LiveLeaseID+".json")
			}
			if err := os.WriteFile(path, []byte("{broken"), 0o600); err != nil {
				t.Fatal(err)
			}
			report, err := harness.store.ProjectStatus()
			if err != nil {
				t.Fatal(err)
			}
			if report.Live != nil || report.LiveError == "" || !reflect.DeepEqual(report.Head, head) || report.RepositoryIdentity == "" {
				t.Fatalf("corrupt live authority hid diagnostics: %#v", report)
			}
		})
	}
}

func TestProjectStatusTracksLifecycleWithoutMutating(t *testing.T) {
	harness := newLifecycleHarness(t)
	before, err := harness.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}

	live, err := harness.store.ProjectStatus()
	if err != nil {
		t.Fatal(err)
	}
	if !live.CanonicalActive || live.SchemaVersion != controllerSchemaVersion || live.RepositoryIdentity != harness.store.Identity().LineageID {
		t.Fatalf("bootstrap status = %#v", live)
	}
	resolvedRepo, err := filepath.EvalSymlinks(harness.repo)
	if err != nil {
		t.Fatal(err)
	}
	if live.Live == nil || live.Live.AttemptID != before.LiveAttemptID || live.Live.LeaseID != before.LiveLeaseID ||
		!live.Live.ExecutionTask.Equal(harness.root) || !live.Live.RootTask.Equal(harness.root) ||
		live.Live.WorkspaceRoot != resolvedRepo || live.Live.Purpose != "root-execution" {
		t.Fatalf("live status = %#v", live.Live)
	}
	if live.Episode != nil || live.Schedule != nil || len(live.Suspensions) != 0 || live.FailureReason != "" {
		t.Fatalf("fresh bootstrap projected episode/suspension/failure state: %#v", live)
	}

	rootSealed := harness.edit(t, harness.source, "status-root.txt", "root work\n")
	episode := harness.planBlockingEpisode(t, rootSealed, harness.blocker, "status-blocker")
	suspended, err := harness.store.SuspendExecution(rootSealed, episode.EpisodeID, episode.Revision)
	if err != nil {
		t.Fatal(err)
	}
	quiescent, err := harness.store.ProjectStatus()
	if err != nil {
		t.Fatal(err)
	}
	if quiescent.Live != nil {
		t.Fatalf("suspended controller still projected live authority: %#v", quiescent.Live)
	}
	if quiescent.Episode == nil || quiescent.Episode.EpisodeID != episode.EpisodeID || quiescent.Episode.Revision != episode.Revision || quiescent.Episode.Error != "" {
		t.Fatalf("episode status = %#v", quiescent.Episode)
	}
	if quiescent.Schedule == nil || quiescent.Schedule.Intent != FindingIntentStartBlockerTask || quiescent.Schedule.NextTaskRef == nil ||
		!quiescent.Schedule.NextTaskRef.Equal(harness.blocker) {
		t.Fatalf("schedule status = %#v", quiescent.Schedule)
	}
	if len(quiescent.Suspensions) != 1 || quiescent.Suspensions[0].SnapshotID != suspended.Suspension.SnapshotID ||
		quiescent.Suspensions[0].TaskPath != harness.root.TaskPath || quiescent.Suspensions[0].Corrupt {
		t.Fatalf("suspension status = %#v", quiescent.Suspensions)
	}

	materialized, err := harness.store.MaterializeExecution(MaterializeExecutionInput{
		ExpectedGeneration: suspended.Head.ControllerGeneration,
		EpisodeID:          episode.EpisodeID,
		EpisodeRevision:    episode.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	blockerStatus, err := harness.store.ProjectStatus()
	if err != nil {
		t.Fatal(err)
	}
	if blockerStatus.Live == nil || !blockerStatus.Live.ExecutionTask.Equal(harness.blocker) ||
		!blockerStatus.Live.RootTask.Equal(harness.root) || blockerStatus.Live.WorkspaceID != materialized.Admission.Workspace.ID ||
		blockerStatus.Live.WorkspaceRoot != materialized.Admission.Workspace.Root || blockerStatus.Live.Purpose != "episode-execution" {
		t.Fatalf("blocker live status = %#v", blockerStatus.Live)
	}

	again, err := harness.store.ProjectStatus()
	if err != nil {
		t.Fatal(err)
	}
	if again.Head.ControllerGeneration != blockerStatus.Head.ControllerGeneration {
		t.Fatalf("status projection mutated the controller: %d -> %d", blockerStatus.Head.ControllerGeneration, again.Head.ControllerGeneration)
	}

	entries, err := os.ReadDir(filepath.Join(harness.store.dir, "suspensions"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("suspension entries = %v %v", entries, err)
	}
	path := filepath.Join(harness.store.dir, "suspensions", entries[0].Name())
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Remove(path)
	})
	corrupt, err := harness.store.ProjectStatus()
	if err != nil {
		t.Fatal(err)
	}
	if len(corrupt.Suspensions) != 1 || !corrupt.Suspensions[0].Corrupt || corrupt.Suspensions[0].File != entries[0].Name() {
		t.Fatalf("corrupt suspension was hidden: %#v", corrupt.Suspensions)
	}
	if corrupt.Live == nil || corrupt.Live.AttemptID != blockerStatus.Live.AttemptID {
		t.Fatal("corrupt suspension hid the live authority projection")
	}
}

func TestProjectStatusReportsPendingTransitionAndFailure(t *testing.T) {
	harness := newLifecycleHarness(t)
	rootSealed := harness.edit(t, harness.source, "pending-root.txt", "root work\n")
	episode := harness.planBlockingEpisode(t, rootSealed, harness.blocker, "pending-blocker")
	op, err := harness.store.planExecutionSuspension(rootSealed, episode.EpisodeID, episode.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if err := harness.store.prepareExecutionOperation(&op, rootSealed.Head); err != nil {
		t.Fatal(err)
	}
	pending, err := harness.store.ProjectStatus()
	if err != nil {
		t.Fatal(err)
	}
	if pending.Head.PendingTransitionID != op.Transition.TransitionID {
		t.Fatalf("pending transition was not projected: %#v", pending.Head)
	}
	if pending.Schedule != nil || pending.Episode != nil {
		t.Fatalf("schedule projected across an uncommitted suspension: %#v", pending.Schedule)
	}
	if _, err := json.Marshal(pending); err != nil {
		t.Fatal(err)
	}
	recovered, err := harness.store.RecoverExecutionOperation(op.Transition.TransitionID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Head.PendingTransitionID != "" {
		t.Fatal("recovery left a pending transition")
	}

	materializeOp, materializeHead, err := harness.store.planExecutionMaterialization(MaterializeExecutionInput{
		ExpectedGeneration: recovered.Head.ControllerGeneration,
		EpisodeID:          episode.EpisodeID,
		EpisodeRevision:    episode.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := harness.store.prepareExecutionOperation(&materializeOp, materializeHead); err != nil {
		t.Fatal(err)
	}
	pendingMaterialize, err := harness.store.ProjectStatus()
	if err != nil {
		t.Fatal(err)
	}
	if pendingMaterialize.Head.PendingTransitionID != materializeOp.Transition.TransitionID {
		t.Fatalf("materialization pending transition was not projected: %#v", pendingMaterialize.Head)
	}
	if pendingMaterialize.Schedule == nil || pendingMaterialize.Schedule.Reason != "controller has a pending transition" {
		t.Fatalf("schedule projected across a pending materialization: %#v", pendingMaterialize.Schedule)
	}
	materialized, err := harness.store.RecoverExecutionOperation(materializeOp.Transition.TransitionID)
	if err != nil {
		t.Fatal(err)
	}

	if materialized.Admission == nil {
		t.Fatal("materialization recovery lost the admission")
	}
	if err := harness.store.FailClosedAdmission(*materialized.Admission, "status projection failure probe", materialized.Admission.Snapshot); err != nil {
		t.Fatal(err)
	}
	failed, err := harness.store.ProjectStatus()
	if err != nil {
		t.Fatal(err)
	}
	if failed.Head.Status != ControllerStatusFailClosed || failed.FailureReason != "status projection failure probe" {
		t.Fatalf("failure status = %#v", failed)
	}
}
