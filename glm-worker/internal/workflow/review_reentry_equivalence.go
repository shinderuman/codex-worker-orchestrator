package workflow

import (
	"crypto/sha256"
	"fmt"
	"reflect"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type equivalentReviewReentry struct {
	completion *state.ParentCompletionOutcome
	reviewer   state.ModelCallLog
	worker     state.ModelCallLog
	source     state.SnapshotDigest
}

func prepareEquivalentReviewReentry(runtime *state.StateStore, repo string) (equivalentReviewReentry, error) {
	completion, err := runtime.CurrentParentCompletionOutcome()
	if err != nil {
		return equivalentReviewReentry{}, err
	}
	if err := validOriginalEquivalentReview(completion); err != nil {
		return equivalentReviewReentry{}, err
	}
	if err := retainedReviewSnapshotsMatch(runtime, completion); err != nil {
		return equivalentReviewReentry{}, err
	}
	worker, reviewer, err := latestEquivalentReviewCalls(runtime)
	if err != nil {
		return equivalentReviewReentry{}, err
	}
	if err := validateRetainedReviewerCall(reviewer, completion.Review); err != nil {
		return equivalentReviewReentry{}, err
	}
	current, err := equivalentReviewSource(repo, completion.Review.Snapshot, completion.Review.Targets)
	if err != nil {
		return equivalentReviewReentry{}, err
	}
	return equivalentReviewReentry{completion: completion, reviewer: reviewer, worker: worker, source: current}, nil
}

func validOriginalEquivalentReview(completion *state.ParentCompletionOutcome) error {
	if !eligibleEquivalentCompletion(completion) {
		return fmt.Errorf("no completed independent HIGH review is available")
	}
	old := completion.Review
	if old.Packet == "" {
		return fmt.Errorf("original canonical Reviewer packet was not retained")
	}
	result, err := packet.ParseStructured([]byte(old.Packet))
	if err != nil {
		return err
	}
	if err := packet.ValidateReviewerResult(result); err != nil {
		return err
	}
	raw, err := result.MachineJSON()
	if err != nil {
		return err
	}
	digest := sha256.Sum256(raw)
	if !sameEquivalentReviewerPacket(result, old, digest) {
		return fmt.Errorf("original canonical Reviewer packet has changed")
	}
	if !sameReviewSource(*completion.Source, old.Snapshot) {
		return fmt.Errorf("original accepted source and Review differ")
	}
	return nil
}

func eligibleEquivalentCompletion(completion *state.ParentCompletionOutcome) bool {
	if completion == nil || completion.Review == nil || completion.Source == nil {
		return false
	}
	return completion.Terminal == state.SessionRotationTerminalAccept &&
		completion.Risk == string(packet.RiskHigh) && completion.Review.Proof != nil &&
		completion.Review.SourceEquivalence == nil
}

func sameEquivalentReviewerPacket(result packet.Result, binding *state.ParentReviewBinding, digest [sha256.Size]byte) bool {
	return fmt.Sprintf("%x", digest) == binding.PacketSHA256 &&
		result.Status == packet.StatusNeedsSolReview && result.Risk == packet.RiskHigh &&
		reflect.DeepEqual(result.Targets, binding.Targets) && result.SolQuestion == binding.SolQuestion
}

func validLatestEquivalentWorker(worker state.ModelCallLog, taskID string) bool {
	return worker.CallID != "" && worker.TaskID == taskID &&
		worker.Outcome == acceptedResultCallOutcome && worker.PacketStatus == string(packet.StatusImplemented) &&
		(worker.Response == "" || savedWorkerReviewContextMatches(worker, taskID)) &&
		worker.ResponseSHA256 != ""
}

func validLatestEquivalentReviewer(reviewer state.ModelCallLog, taskID string) bool {
	return reviewer.CallID != "" && reviewer.Outcome == acceptedResultCallOutcome &&
		reviewer.ReadOnly && reviewer.PacketStatus == string(packet.StatusNeedsSolReview) &&
		reviewer.TaskID == taskID && reviewer.SessionID != "" && reviewer.ResponseSHA256 != ""
}

func retainedReviewSnapshotsMatch(runtime *state.StateStore, completion *state.ParentCompletionOutcome) error {
	start, err := runtime.LoadReviewStartSnapshot()
	if err != nil {
		return fmt.Errorf("original review-start snapshot is missing: %w", err)
	}
	if !sameReviewSource(snapshotFromGit(start), completion.Review.Snapshot) {
		return fmt.Errorf("original review-start source differs from accepted source")
	}
	end, err := runtime.LoadWorkerEndSnapshot()
	if err != nil {
		return fmt.Errorf("original Worker snapshot is missing: %w", err)
	}
	if !sameReviewSource(snapshotFromGit(end), completion.Review.Snapshot) {
		return fmt.Errorf("worker source differs from the independently reviewed source")
	}
	return nil
}

func snapshotFromGit(snapshot state.GitSnapshot) state.SnapshotDigest {
	return state.SnapshotDigest{Head: snapshot.Head, IndexDigest: snapshot.IndexDigest, WorktreeDigest: snapshot.WorktreeDigest}
}

func latestEquivalentReviewCalls(runtime *state.StateStore) (state.ModelCallLog, state.ModelCallLog, error) {
	taskID, err := runtime.TaskID()
	if err != nil {
		return state.ModelCallLog{}, state.ModelCallLog{}, err
	}
	logs, err := runtime.ReadModelCallLogs(taskID)
	if err != nil {
		return state.ModelCallLog{}, state.ModelCallLog{}, err
	}
	var worker, reviewer state.ModelCallLog
	for _, call := range logs {
		if call.CallType != state.CallTypeTask {
			continue
		}
		switch call.Role {
		case state.WorkerRole:
			worker = call
			reviewer = state.ModelCallLog{}
		case state.ReviewerRole:
			reviewer = call
		}
	}
	if !validLatestEquivalentWorker(worker, taskID) || !validLatestEquivalentReviewer(reviewer, taskID) {
		return state.ModelCallLog{}, state.ModelCallLog{}, fmt.Errorf("independent Reviewer/Worker model provenance unavailable")
	}
	return worker, reviewer, nil
}

func validateRetainedReviewerCall(reviewer state.ModelCallLog, binding *state.ParentReviewBinding) error {
	if reviewer.Response == "" {
		if reviewer.ReviewerReportedRisk != string(packet.RiskHigh) {
			return fmt.Errorf("content-free Reviewer telemetry lacks authenticated HIGH risk")
		}
		return nil
	}
	sha := sha256.Sum256([]byte(reviewer.Response))
	if fmt.Sprintf("%x", sha) != reviewer.ResponseSHA256 {
		return fmt.Errorf("retained Reviewer response bytes differ from its log digest")
	}
	result, err := packet.ParseStructured([]byte(reviewer.Response))
	if err != nil {
		return err
	}
	if err := packet.ValidateReviewerResult(result); err != nil {
		return err
	}
	raw, err := result.MachineJSON()
	if err != nil {
		return err
	}
	digest := sha256.Sum256(raw)
	if !sameEquivalentReviewerPacket(result, binding, digest) || result.SolQuestion == "" {
		return fmt.Errorf("independent Reviewer packet differs from the accepted canonical binding")
	}
	return nil
}

func sameReviewSource(a, b state.SnapshotDigest) bool {
	return a.Head == b.Head && a.IndexDigest == b.IndexDigest && a.WorktreeDigest == b.WorktreeDigest &&
		a.Head != "" && a.IndexDigest != "" && a.WorktreeDigest != ""
}

func reenterWithEquivalentReview(runtime *state.StateStore, repo string, authority state.ControllerReentryAuthority) (bool, error) {
	evidence, err := prepareEquivalentReviewReentry(runtime, repo)
	if err != nil {
		return false, fmt.Errorf("temporary equivalence diagnosis: %w", err)
	}
	return runtime.ReenterEquivalentReviewFromController(authority, evidence.completion, evidence.reviewer, evidence.worker, evidence.source)
}

func admittedEquivalentReview(runtime *state.StateStore, attempt controller.AttemptRecord, repoRoot string) (bool, error) {
	if runtime.TaskStatus() != state.TaskStatusWaitingSolReview {
		return false, nil
	}
	binding, err := runtime.LoadControllerRuntimeBinding()
	if err != nil {
		return false, err
	}
	if binding.AttemptID != attempt.AttemptID || binding.TaskPath != attempt.SemanticTaskRef.TaskPath ||
		binding.TaskContractDigest != attempt.SemanticTaskRef.ContractDigest {
		return false, nil
	}
	return equivalentReviewMatchesCurrentSource(runtime, repoRoot)
}

func equivalentReviewMatchesCurrentSource(runtime *state.StateStore, repoRoot string) (bool, error) {
	review, err := runtime.CurrentParentReviewBinding()
	if err != nil {
		return false, err
	}
	if review == nil || review.SourceEquivalence == nil {
		return false, nil
	}
	open, err := runtime.CurrentParentReview()
	if err != nil {
		return false, err
	}
	if open == nil || open.Role != string(state.ReviewerRole) ||
		open.Risk != string(packet.RiskHigh) || open.PacketStatus != string(packet.StatusNeedsSolReview) {
		return false, nil
	}
	now, err := state.CaptureGitSnapshot(repoRoot)
	if err != nil {
		return false, err
	}
	return sameReviewSource(review.Snapshot, snapshotFromGit(now)), nil
}
