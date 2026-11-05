package app

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentevidence"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

type postPullMetadataReviewFixture struct {
	cfg       config.AppConfig
	store     *controller.Store
	runtime   *state.StateStore
	accepted  controller.ExecutionOperationResult
	candidate controller.AcceptedCandidate
	original  *state.ParentCompletionOutcome
	taskID    string
}

func newPostPullMetadataReviewFixture(t *testing.T) postPullMetadataReviewFixture {
	t.Helper()
	cfg, store, accepted, candidate, runtime := sealedCandidateReentryFixture(t)
	if err := runtime.SetTaskStatus(state.TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Write("repo-root", cfg.RepoRoot); err != nil {
		t.Fatal(err)
	}
	snapshot, err := state.CaptureGitSnapshot(cfg.RepoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.SaveWorkerEndSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	if err := runtime.SaveReviewStartSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	source := state.SnapshotDigest{Head: snapshot.Head, IndexDigest: snapshot.IndexDigest, WorktreeDigest: snapshot.WorktreeDigest}
	result := packet.Result{
		Status: packet.StatusNeedsSolReview, Risk: packet.RiskHigh,
		Summary: "independent reviewer evidence", RequirementCoverage: "original requirements preserved",
		Invariants: "source remains fixed", TestEvidence: "scripted regression proof",
		Issues: "unresolved HIGH issue retained", ResidualRisk: "HIGH pending parent decision",
		SolQuestion: "Inspect the original target and decide whether to accept.",
		Targets: []string{"IMPLEMENTATION_TASKS/root.md:1"},
	}
	if err := packet.ValidateReviewerResult(result); err != nil {
		t.Fatal(err)
	}
	if err := runtime.RecordSolResultWithReviewSnapshot(result, state.ParentReviewProducer{
		Role: string(state.ReviewerRole), Model: "scripted-reviewer",
	}, source); err != nil {
		t.Fatal(err)
	}
	taskID, err := runtime.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	worker := state.ModelCallLog{
		TaskID: taskID, CallID: "original-worker-call", CallType: state.CallTypeTask,
		Role: state.WorkerRole, SessionID: "original-worker-session", Outcome: "success",
		PacketStatus: string(packet.StatusImplemented), ResponseSHA256: "original-worker-response-digest",
	}
	machine, err := result.MachineJSON()
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(machine)
	reviewer := state.ModelCallLog{
		TaskID: taskID, CallID: "original-reviewer-call", CallType: state.CallTypeTask,
		Role: state.ReviewerRole, ModelAlias: "scripted-reviewer", SessionID: "original-reviewer-session",
		ReadOnly: true, Outcome: "success", PacketStatus: string(packet.StatusNeedsSolReview),
		Response: string(machine), ResponseSHA256: fmt.Sprintf("%x", hash),
		ReviewerReportedRisk: string(packet.RiskHigh),
	}
	runtime.RecordModelCallLog(worker)
	runtime.RecordModelCallLog(reviewer)
	binding, err := runtime.CurrentParentReviewBinding()
	if err != nil || binding == nil {
		t.Fatalf("original HIGH Review binding: %#v %v", binding, err)
	}
	if err := runtime.MarkParentReviewEvidence(binding.ID, "original-parent-evidence", []state.ParentReviewEvidenceClaim{{
		Kind: "source", Digest: "original-target-digest", Locator: "IMPLEMENTATION_TASKS/root.md:1-1",
	}}); err != nil {
		t.Fatal(err)
	}
	if ok, err := runtime.AcceptParentReview(); err != nil || !ok {
		t.Fatalf("initial independent HIGH accept: %v %v", ok, err)
	}
	original, err := runtime.CurrentParentCompletionOutcome()
	if err != nil || original == nil || original.Review == nil || original.Source == nil {
		t.Fatalf("original completion missing packet: %#v %v", original, err)
	}
	return postPullMetadataReviewFixture{cfg: cfg, store: store, runtime: runtime, accepted: accepted,
		candidate: candidate, original: original, taskID: taskID}
}

func (f postPullMetadataReviewFixture) advanceNextAndMaterialize(t *testing.T, mutate func(string)) controller.ExecutionOperationResult {
	t.Helper()
	repo := f.cfg.RepoRoot
	planPath := filepath.Join(repo, state.ParentPlanFile)
	before, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	newTask := "IMPLEMENTATION_TASKS/post-pull-next.md"
	writeAppTestFile(t, repo, newTask, "# NEXT\n\n## Contract\n\nMetadata-only successor.\n\n## Dependencies\n\nnone\n\n## External feasibility\n\nstatus: not-applicable\n")
	nextLine := "- `"+newTask+"`\n"
	writeAppTestFile(t, repo, state.ParentPlanFile, strings.Replace(string(before), "## NEXT\n", "## NEXT\n\n"+nextLine, 1))
	if mutate != nil {
		mutate(repo)
	}
	runControllerActivationGit(t, repo, "add", "--all")
	runControllerActivationGit(t, repo, "commit", "-qm", "add next-only task after independent HIGH review")
	runControllerActivationGit(t, repo, "push", "-q", f.candidate.Policy.Remote, "HEAD:"+f.candidate.Policy.RemoteRef)
	snapshot, err := controller.CaptureWorkspaceSnapshot(repo)
	if err != nil {
		t.Fatal(err)
	}
	rebound, err := f.store.RebindParentSourceCandidate(controller.PublicationInput{
		ExpectedGeneration: f.accepted.Head.ControllerGeneration, CandidateID: f.candidate.CandidateID,
	}, snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	c, err := f.store.LoadAcceptedCandidate(*rebound.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	entered, err := f.store.ReenterAcceptedCandidate(controller.PublicationInput{
		ExpectedGeneration: rebound.Head.ControllerGeneration, CandidateID: c.CandidateID,
	})
	if err != nil {
		t.Fatal(err)
	}
	live, err := workflow.ExecuteControllerExecution(f.cfg, f.store, workflow.ControllerExecutionCommand{
		Action: "materialize", ExpectedGeneration: entered.Head.ControllerGeneration,
		SuspensionID: entered.Suspension.SnapshotID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if live.Admission == nil || !live.ParentSourceReview {
		t.Fatalf("materialization lost parent source Review route: %#v", live)
	}
	return live
}

func countPostPullReviewerCalls(t *testing.T, runtime *state.StateStore, taskID string) int {
	t.Helper()
	logs, err := runtime.ReadModelCallLogs(taskID)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, call := range logs {
		if call.CallType == state.CallTypeTask && call.Role == state.ReviewerRole {
			count++
		}
	}
	return count
}

func TestPostPullMetadataEquivalentReviewReentersThroughMaterializeAndAcceptsNewEvidence(t *testing.T) {
	f := newPostPullMetadataReviewFixture(t)
	callsBefore := countPostPullReviewerCalls(t, f.runtime, f.taskID)
	live := f.advanceNextAndMaterialize(t, nil)
	binding, err := f.runtime.CurrentParentReviewBinding()
	if err != nil || binding == nil || binding.SourceEquivalence == nil {
		t.Fatalf("materialized Review did not preserve source equivalence: %#v %v", binding, err)
	}
	if binding.ID != f.original.Review.ID || binding.Packet != f.original.Review.Packet ||
		binding.PacketSHA256 != f.original.Review.PacketSHA256 ||
		!reflect.DeepEqual(binding.Targets, f.original.Review.Targets) ||
		binding.SourceEquivalence.ReviewerCallID != "original-reviewer-call" ||
		binding.SourceEquivalence.WorkerCallID != "original-worker-call" ||
		binding.SourceEquivalence.OriginalSnapshot != *f.original.Source ||
		binding.Proof != nil || binding.Coverage != nil {
		t.Fatalf("original packet, issues, call IDs or parent evidence were corrupted: %#v", binding)
	}
	open, err := f.runtime.CurrentParentReview()
	if err != nil || open == nil || open.Risk != string(packet.RiskHigh) ||
		open.PacketStatus != string(packet.StatusNeedsSolReview) {
		t.Fatalf("original HIGH Review was lowered: %#v %v", open, err)
	}
	if !strings.Contains(binding.Packet, "unresolved HIGH issue retained") {
		t.Fatal("original unresolved issues were not preserved")
	}
	if ready, err := f.runtime.ParentReviewAcceptReady(); err != nil || ready {
		t.Fatalf("old parent proof was accepted on new source: %v %v", ready, err)
	}
	if accepted, err := f.runtime.AcceptParentReview(); accepted || err == nil {
		t.Fatalf("stale parent evidence admitted accept: %v %v", accepted, err)
	}
	laneCfg := f.cfg
	laneCfg.RepoRoot = live.Admission.Workspace.Root
	manifest := writeParentReviewEvidenceManifest(t, parentevidence.Manifest{
		Version: parentevidence.ManifestVersion,
		Reason: "re-prove independent HIGH target against materialized source",
		Source: []parentevidence.SourceRequest{{
			Question: "inspect original target on new source",
			Path: "IMPLEMENTATION_TASKS/root.md", LineStart: 1, LineEnd: 1, BudgetBytes: 4096,
		}},
	})
	if err := printParentEvidence(Command{EvidenceManifest: manifest}, laneCfg, f.runtime, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if ready, err := f.runtime.ParentReviewAcceptReady(); err != nil || !ready {
		t.Fatalf("new parent proof did not restore accept: %v %v", ready, err)
	}
	if err := Execute(Command{Mode: ModeAccept}, laneCfg, nil, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	after, err := f.runtime.CurrentParentCompletionOutcome()
	if err != nil || after == nil || after.Review == nil || after.Review.Packet != f.original.Review.Packet {
		t.Fatalf("canonical accept replaced original Reviewer packet: %#v %v", after, err)
	}
	if countPostPullReviewerCalls(t, f.runtime, f.taskID) != callsBefore {
		t.Fatal("source equivalence created an additional Reviewer model call")
	}
}

func TestPostPullMetadataChangedDependencyFallsBackToFreshReview(t *testing.T) {
	f := newPostPullMetadataReviewFixture(t)
	originalCalls := countPostPullReviewerCalls(t, f.runtime, f.taskID)
	f.advanceNextAndMaterialize(t, func(repo string) {
		writeAppTestFile(t, repo, "new-dependent-source.go", "package regression\n")
	})
	binding, err := f.runtime.CurrentParentReviewBinding()
	if err != nil {
		t.Fatal(err)
	}
	if binding != nil && binding.SourceEquivalence != nil {
		t.Fatal("changed dependency reused original Reviewer packet")
	}
	if countPostPullReviewerCalls(t, f.runtime, f.taskID) != originalCalls {
		t.Fatal("falling back to ordinary Review itself manufactured a model call")
	}
}
