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

// prepareEquivalentReviewReentry only accepts the latest independent reviewer
// following the last successful Worker in the original attempt. The original
// full packet must be available and match the canonical parent binding.
// Missing telemetry or an ambiguous evidence chain requires ordinary re-review.
func prepareEquivalentReviewReentry(runtime *state.StateStore, repo string) (equivalentReviewReentry, error) {
	completion, err := runtime.CurrentParentCompletionOutcome()
	if err != nil {
		return equivalentReviewReentry{}, err
	}
	if completion == nil || completion.Terminal != state.SessionRotationTerminalAccept ||
		completion.Risk != string(packet.RiskHigh) || completion.Review == nil ||
		completion.Source == nil || completion.Review.Proof == nil ||
		completion.Review.SourceEquivalence != nil {
		return equivalentReviewReentry{}, fmt.Errorf("no completed independent HIGH review is available")
	}
	old := completion.Review
	if len(old.Packet) == 0 {
		return equivalentReviewReentry{}, fmt.Errorf("original canonical Reviewer packet was not retained")
	}
	originalPacket, err := packet.ParseStructured(old.Packet)
	if err != nil {
		return equivalentReviewReentry{}, err
	}
	if err := packet.ValidateReviewerResult(originalPacket); err != nil {
		return equivalentReviewReentry{}, err
	}
	machine, err := originalPacket.MachineJSON()
	if err != nil {
		return equivalentReviewReentry{}, err
	}
	originalSHA := sha256.Sum256(machine)
	if fmt.Sprintf("%x", originalSHA) != old.PacketSHA256 ||
		originalPacket.Status != packet.StatusNeedsSolReview || originalPacket.Risk != packet.RiskHigh ||
		!reflect.DeepEqual(originalPacket.Targets, old.Targets) ||
		originalPacket.SolQuestion != old.SolQuestion {
		return equivalentReviewReentry{}, fmt.Errorf("original canonical Reviewer packet has changed")
	}
	if !sameReviewSource(*completion.Source, old.Snapshot) {
		return equivalentReviewReentry{}, fmt.Errorf("original accepted source and Review differ")
	}
	reviewStart, err := runtime.LoadReviewStartSnapshot()
	if err != nil || !sameReviewSource(state.SnapshotDigest{
		Head: reviewStart.Head, IndexDigest: reviewStart.IndexDigest,
		WorktreeDigest: reviewStart.WorktreeDigest,
	}, old.Snapshot) {
		return equivalentReviewReentry{}, fmt.Errorf("original review-start snapshot is missing or stale: %v", err)
	}
	workerEnd, err := runtime.LoadWorkerEndSnapshot()
	if err != nil || !sameReviewSource(state.SnapshotDigest{
		Head: workerEnd.Head, IndexDigest: workerEnd.IndexDigest,
		WorktreeDigest: workerEnd.WorktreeDigest,
	}, old.Snapshot) {
		return equivalentReviewReentry{}, fmt.Errorf("Worker source differs from the independently reviewed source: %v", err)
	}
	taskID, err := runtime.TaskID()
	if err != nil {
		return equivalentReviewReentry{}, err
	}
	logs, err := runtime.ReadModelCallLogs(taskID)
	if err != nil {
		return equivalentReviewReentry{}, err
	}
	var worker, reviewer state.ModelCallLog
	for _, call := range logs {
		if call.CallType != state.CallTypeTask {
			continue
		}
		if call.Role == state.WorkerRole {
			worker = call
			reviewer = state.ModelCallLog{}
		}
		if call.Role == state.ReviewerRole {
			reviewer = call
		}
	}
	if worker.CallID == "" || worker.Outcome != acceptedResultCallOutcome ||
		worker.PacketStatus != string(packet.StatusImplemented) ||
		(worker.Response != "" && !savedWorkerReviewContextMatches(worker, taskID)) || worker.ResponseSHA256 == "" ||
		reviewer.CallID == "" || reviewer.Outcome != "success" ||
		!reviewer.ReadOnly || reviewer.PacketStatus != string(packet.StatusNeedsSolReview) ||
		reviewer.TaskID != taskID || reviewer.SessionID == "" ||
		reviewer.ResponseSHA256 == "" {
		return equivalentReviewReentry{}, fmt.Errorf("independent Reviewer/Worker model provenance unavailable")
	}
	// Normal telemetry can omit content while retaining the verified
	// packet binding, response digest, risk, and independent model-call
	// authority. If raw content is available, additionally verify it in full.
	if reviewer.Response != "" {
		sha := sha256.Sum256([]byte(reviewer.Response))
		if fmt.Sprintf("%x", sha) != reviewer.ResponseSHA256 {
			return equivalentReviewReentry{}, fmt.Errorf("retained Reviewer response bytes differ from its log digest")
		}
		result, err := packet.ParseStructured([]byte(reviewer.Response))
		if err != nil {
			return equivalentReviewReentry{}, err
		}
		if err := packet.ValidateReviewerResult(result); err != nil {
			return equivalentReviewReentry{}, err
		}
		raw, err := result.MachineJSON()
		if err != nil {
			return equivalentReviewReentry{}, err
		}
		canonicalSHA := sha256.Sum256(raw)
		if fmt.Sprintf("%x", canonicalSHA) != old.PacketSHA256 ||
			result.Status != packet.StatusNeedsSolReview || result.Risk != packet.RiskHigh ||
			!reflect.DeepEqual(result.Targets, old.Targets) || result.SolQuestion != old.SolQuestion ||
			result.SolQuestion == "" {
			return equivalentReviewReentry{}, fmt.Errorf("independent Reviewer packet differs from the accepted canonical binding")
		}
	} else if reviewer.ReviewerReportedRisk != string(packet.RiskHigh) {
		return equivalentReviewReentry{}, fmt.Errorf("content-free Reviewer telemetry lacks authenticated HIGH risk")
	}
	current, err := equivalentReviewSource(repo, old.Snapshot, old.Targets)
	if err != nil {
		return equivalentReviewReentry{}, err
	}
	return equivalentReviewReentry{completion: completion, reviewer: reviewer, worker: worker, source: current}, nil
}

func sameReviewSource(a, b state.SnapshotDigest) bool {
	return a.Head == b.Head && a.IndexDigest == b.IndexDigest && a.WorktreeDigest == b.WorktreeDigest &&
		a.Head != "" && a.IndexDigest != "" && a.WorktreeDigest != ""
}

// reenterWithEquivalentReview attempts only a well-proven reuse. All rejected
// cases run through the existing canonical controller reentry and fresh Review.
func reenterWithEquivalentReview(runtime *state.StateStore, repo string, authority state.ControllerReentryAuthority) (bool, error) {
	evidence, err := prepareEquivalentReviewReentry(runtime, repo)
	if err != nil {
		return false, nil
	}
	reentered, err := runtime.ReenterEquivalentReviewFromController(authority, evidence.completion, evidence.reviewer, evidence.worker, evidence.source)
	if err != nil {
		return false, err
	}
	return reentered, nil
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
	review, err := runtime.CurrentParentReviewBinding()
	if err != nil || review == nil || review.SourceEquivalence == nil {
		return false, err
	}
	open, err := runtime.CurrentParentReview()
	if err != nil || open == nil || open.Role != string(state.ReviewerRole) ||
		open.Risk != string(packet.RiskHigh) || open.PacketStatus != string(packet.StatusNeedsSolReview) {
		return false, err
	}
	now, err := state.CaptureGitSnapshot(repoRoot)
	if err != nil {
		return false, err
	}
	return sameReviewSource(review.Snapshot, state.SnapshotDigest{Head: now.Head, IndexDigest: now.IndexDigest, WorktreeDigest: now.WorktreeDigest}), nil
}
