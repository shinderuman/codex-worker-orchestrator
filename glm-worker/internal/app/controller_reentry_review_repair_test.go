package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

type reentryReviewRepairFixture struct {
	cfg     config.AppConfig
	store   *controller.Store
	runtime *state.StateStore
	admit   controller.Admission
}

func reentryReviewRepairAppFixture(t *testing.T) reentryReviewRepairFixture {
	t.Helper()
	cfg, store, accepted, candidate, runtime := sealedCandidateReentryFixture(t)
	reentered, err := store.ReenterAcceptedCandidate(controller.PublicationInput{ExpectedGeneration: accepted.Head.ControllerGeneration, CandidateID: candidate.CandidateID})
	if err != nil {
		t.Fatal(err)
	}
	lane, err := executeControllerExecution(cfg, store, controllerExecutionCommand{Action: "materialize", ExpectedGeneration: reentered.Head.ControllerGeneration, SuspensionID: reentered.Suspension.SnapshotID})
	if err != nil {
		t.Fatal(err)
	}
	source := *lane.Admission
	bound := cfg
	bound.RepoRoot = source.Workspace.Root
	if err := state.CaptureGitBaseline(bound, runtime); err != nil {
		t.Fatal(err)
	}
	if err := runtime.SaveControllerRuntimeBinding(state.ControllerRuntimeBinding{
		AttemptID:          source.Attempt.AttemptID,
		TaskPath:           source.Attempt.SemanticTaskRef.TaskPath,
		TaskContractDigest: source.Attempt.SemanticTaskRef.ContractDigest,
	}); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Write("repo-root", source.Workspace.Root); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Write("last-request", "reentry review repair original request"); err != nil {
		t.Fatal(err)
	}
	taskID, err := runtime.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	response, err := json.Marshal(packet.Result{Status: packet.StatusImplemented, Risk: packet.RiskLow, Summary: "worker result", RequirementCoverage: "covered", Tests: "pass", Unverified: "none"})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(response)
	runtime.RecordModelCallLog(state.ModelCallLog{
		Version: state.ModelCallLogVersion, CallType: state.CallTypeTask, TaskID: taskID,
		CallID: "reentry-review-repair-worker-final", SessionID: "reentry-review-repair-worker",
		Role: state.WorkerRole, Outcome: "success", PacketStatus: string(packet.StatusImplemented),
		Response: string(response), ResponseSHA256: hex.EncodeToString(digest[:]),
	})
	if _, err := executeControllerExecution(cfg, store, controllerExecutionCommand{Action: "prepare-reentry-review", ExpectedGeneration: source.Head.ControllerGeneration}); err != nil {
		t.Fatal(err)
	}
	if runtime.TaskStatus() != state.TaskStatusInterrupted {
		t.Fatalf("reentry review preparation left the runtime at %s", runtime.TaskStatus())
	}
	queued, err := runtime.LoadResumeCheckpoint()
	if err != nil || queued.Role != state.ReviewerRole || !queued.ReadOnly {
		t.Fatalf("reentered reviewer checkpoint: %#v %v", queued, err)
	}
	return reentryReviewRepairFixture{cfg: cfg, store: store, runtime: runtime, admit: source}
}

func (f reentryReviewRepairFixture) revisePrimaryTask(t *testing.T, amendment string) (controller.WorkspaceSnapshot, controller.SemanticTaskRef) {
	t.Helper()
	taskPath := f.admit.Attempt.SemanticTaskRef.TaskPath
	task, err := os.ReadFile(filepath.Join(f.cfg.RepoRoot, taskPath))
	if err != nil {
		t.Fatal(err)
	}
	task = append(task, []byte(amendment)...)
	writeAppTestFile(t, f.cfg.RepoRoot, taskPath, string(task))
	writeAppTestFile(t, f.cfg.RepoRoot, "parent-fix.txt", "parent codex fix "+amendment)
	snapshot, err := controller.CaptureWorkspaceSnapshot(f.cfg.RepoRoot)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot, controller.SemanticTaskRef{TaskPath: taskPath, ContractDigest: fmt.Sprintf("%x", sha256.Sum256(task))}
}

func TestControllerReentryReviewRepairImportsRevisedContractOnLiveLane(t *testing.T) {
	f := reentryReviewRepairAppFixture(t)
	snapshot, revised := f.revisePrimaryTask(t, "\n## Amendments\n\n親Codexの改訂と独立GLM Review。\n")
	headBefore, err := f.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	stale := controllerExecutionCommand{Action: "update-reentry-review", ExpectedGeneration: headBefore.ControllerGeneration, SourceSnapshotID: "stale-parent-snapshot", RevisedTask: &revised}
	if _, err := executeControllerExecution(f.cfg, f.store, stale); err == nil {
		t.Fatal("stale parent source snapshot was imported")
	}
	foreign := revised
	foreign.TaskPath = "IMPLEMENTATION_TASKS/successor.md"
	if _, err := executeControllerExecution(f.cfg, f.store, controllerExecutionCommand{Action: "update-reentry-review", ExpectedGeneration: headBefore.ControllerGeneration, SourceSnapshotID: snapshot.ID, RevisedTask: &foreign}); err == nil {
		t.Fatal("foreign Task authority was imported into the live lane")
	}
	afterRejection, err := f.store.LoadHead()
	if err != nil || !reflect.DeepEqual(afterRejection, headBefore) {
		t.Fatal("rejected reentry review repair mutated controller authority")
	}
	updated, err := executeControllerExecution(f.cfg, f.store, controllerExecutionCommand{Action: "update-reentry-review", ExpectedGeneration: headBefore.ControllerGeneration, SourceSnapshotID: snapshot.ID, RevisedTask: &revised})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Head.LiveAttemptID != f.admit.Attempt.AttemptID || updated.Head.LiveLeaseID == f.admit.Lease.LeaseID {
		t.Fatalf("review repair lost the attempt or rotated the lease: %#v", updated.Head)
	}
	if updated.Head.RootTaskRef == nil || !updated.Head.RootTaskRef.Equal(revised) || updated.Head.ExecutionTaskRef == nil || !updated.Head.ExecutionTaskRef.Equal(revised) {
		t.Fatalf("review repair did not bind the revised same-path contract: %#v", updated.Head)
	}
	laneTask, err := os.ReadFile(filepath.Join(f.admit.Workspace.Root, revised.TaskPath))
	if err != nil || !bytes.Contains(laneTask, []byte("親Codexの改訂と独立GLM Review")) {
		t.Fatalf("revised contract was not imported into the live lane: %v", err)
	}
	if _, err := os.Stat(filepath.Join(f.admit.Workspace.Root, "parent-fix.txt")); err != nil {
		t.Fatalf("parent fix was not imported into the live lane: %v", err)
	}
	binding, err := f.runtime.LoadControllerRuntimeBinding()
	if err != nil || binding.AttemptID != f.admit.Attempt.AttemptID || binding.TaskContractDigest != revised.ContractDigest {
		t.Fatalf("runtime binding after repair: %#v %v", binding, err)
	}
	checkpoint, err := f.runtime.LoadResumeCheckpoint()
	if err != nil || checkpoint.Role != state.ReviewerRole || !checkpoint.ReadOnly || checkpoint.StopGitSnapshot == nil {
		t.Fatalf("review repair lost the retained reviewer checkpoint: %#v %v", checkpoint, err)
	}
	if !bytes.Contains([]byte(checkpoint.Prompt), []byte("parent-fix.txt: new-boundary")) || checkpoint.Prompt != checkpoint.OriginalPrompt {
		t.Fatal("reviewer prompt does not describe the imported source boundary")
	}
	patch, err := f.runtime.Read("review-current-task.patch")
	if err != nil || !bytes.Contains([]byte(patch), []byte("parent codex fix")) {
		t.Fatalf("review navigation patch does not contain the imported source: %v", err)
	}
	if _, err := f.store.ReentryReviewRepairEvidence(updated.TransitionID); err != nil {
		t.Fatalf("reentry review repair lost its runtime identity: %v", err)
	}
}

func TestControllerReentryReviewRepairRecoversPartialRuntimeCompletion(t *testing.T) {
	f := reentryReviewRepairAppFixture(t)
	snapshot, revised := f.revisePrimaryTask(t, "\n## Amendments\n\n親Codex実装と独立GLM Review。\n")
	headBefore, err := f.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := executeControllerExecution(f.cfg, f.store, controllerExecutionCommand{Action: "update-reentry-review", ExpectedGeneration: headBefore.ControllerGeneration, SourceSnapshotID: snapshot.ID, RevisedTask: &revised}); err != nil {
		t.Fatal(err)
	}
	completed, err := f.runtime.LoadResumeCheckpoint()
	if err != nil {
		t.Fatal(err)
	}
	secondSnapshot, secondRevision := f.revisePrimaryTask(t, "\n## Amendments\n\nreview修正後の追加改訂。\n")
	bound, err := controller.ParentContinuationConfig(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	wf := workflow.NewWorkflow(bound, f.runtime, nil, io.Discard)
	before, err := wf.CaptureReentryReviewRepairEvidence()
	if err != nil {
		t.Fatal(err)
	}
	admission, err := currentControllerSemanticAdmission(f.cfg, f.store)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := f.store.PutEvidenceObject("runtime-artifact", "application/json", admission.Attempt.AttemptID+":reentry-review-repair:"+secondSnapshot.ID, true, before)
	if err != nil {
		t.Fatal(err)
	}
	partial, err := f.store.ImportParentSource(admission, secondSnapshot.ID, ref, secondRevision)
	if err != nil {
		t.Fatal(err)
	}
	if partial.Head.RootTaskRef == nil || !partial.Head.RootTaskRef.Equal(secondRevision) {
		t.Fatalf("partial repair lost the second revision authority: %#v", partial.Head)
	}
	unfinished, err := f.runtime.LoadResumeCheckpoint()
	if err != nil || !reflect.DeepEqual(unfinished, completed) {
		t.Fatal("controller-side repair completed the runtime without its journal recovery")
	}
	laneTask, err := os.ReadFile(filepath.Join(f.admit.Workspace.Root, secondRevision.TaskPath))
	if err != nil || !bytes.Contains(laneTask, []byte("review修正後の追加改訂")) {
		t.Fatalf("second revision was not applied to the lane: %v", err)
	}
	recovered, err := executeControllerExecution(f.cfg, f.store, controllerExecutionCommand{Action: "recover", TransitionID: partial.TransitionID})
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Head.RootTaskRef == nil || !recovered.Head.RootTaskRef.Equal(secondRevision) {
		t.Fatalf("runtime completion recovery lost the second revision authority: %#v", recovered.Head)
	}
	finalCheckpoint, err := f.runtime.LoadResumeCheckpoint()
	if err != nil || reflect.DeepEqual(finalCheckpoint, completed) || finalCheckpoint.Role != state.ReviewerRole || !finalCheckpoint.ReadOnly {
		t.Fatalf("journal recovery did not revise the retained reviewer checkpoint: %#v %v", finalCheckpoint, err)
	}
	binding, err := f.runtime.LoadControllerRuntimeBinding()
	if err != nil || binding.TaskContractDigest != secondRevision.ContractDigest {
		t.Fatalf("runtime binding after recovery: %#v %v", binding, err)
	}
}
