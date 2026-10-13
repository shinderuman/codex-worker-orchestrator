package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type qualityMutationGuard struct {
	store     *controller.Store
	admission controller.Admission
	before    controller.WorkspaceSnapshot
}

const qualityFixEvidenceStateKey = "quality-fix-evidence"

func (w *Workflow) runQualityGate() (harnesslint.Report, error) {
	guard, err := w.admitQualityGateMutation()
	if err != nil {
		return harnesslint.Report{}, err
	}
	before := w.captureQualityGateBefore(guard)
	report, err := w.qualityGate(w.config.RepoRoot)
	if err != nil {
		return report, w.verifyQualityGateAbort(guard, before, err)
	}
	if err := w.recordQualityGateMutation(guard, before, report); err != nil {
		return harnesslint.Report{}, err
	}
	return report, nil
}

func (w *Workflow) admitQualityGateMutation() (*qualityMutationGuard, error) {
	store, head, active, err := w.liveControllerStore()
	if err != nil || !active {
		return nil, err
	}
	authority, err := controller.MutationAuthorityFromHead(head)
	if err != nil {
		return nil, err
	}
	workspace, err := controller.ResolveWorkspaceIdentity(w.config.RepoRoot, store.Identity())
	if err != nil {
		return nil, err
	}
	snapshot, err := controller.CaptureWorkspaceSnapshot(w.config.RepoRoot)
	if err != nil {
		return nil, err
	}
	admission, err := store.AdmitMutationOrFailClosed(authority, workspace, snapshot)
	if err != nil {
		return nil, err
	}
	if err := store.StageQualityMutation(admission); err != nil {
		return nil, err
	}
	return &qualityMutationGuard{store: store, admission: admission, before: snapshot}, nil
}

func (w *Workflow) captureQualityGateBefore(guard *qualityMutationGuard) state.GitSnapshot {
	if guard != nil {
		return workspaceRuntimeSnapshot(guard.before)
	}
	before, err := w.captureSnapshot(w.config.RepoRoot)
	if err != nil {
		return state.GitSnapshot{}
	}
	return before
}

func (w *Workflow) verifyQualityGateAbort(guard *qualityMutationGuard, before state.GitSnapshot, gateErr error) error {
	if drift := w.qualityGateNoFixDrift(guard, before); drift != nil {
		return fmt.Errorf("machine quality gate failed after changing the workspace without fix provenance: %w", errors.Join(gateErr, drift))
	}
	if guard != nil {
		if err := guard.store.ClearQualityMutationIntent(guard.admission); err != nil {
			return err
		}
	}
	return gateErr
}

func (w *Workflow) recordQualityGateMutation(guard *qualityMutationGuard, before state.GitSnapshot, report harnesslint.Report) error {
	if report.Fixed == 0 {
		if guard != nil {
			return w.verifyQualityGateNoFix(guard, before)
		}
		return nil
	}
	if guard != nil {
		return w.recordControllerQualityFixMutation(guard, report)
	}
	if report.FixEvidence == nil {
		return nil
	}
	if _, err := w.persistQualityFixEvidence(report); err != nil {
		return err
	}
	return w.syncWorkerEndSnapshotToQualityFix(report.FixEvidence)
}

func (w *Workflow) recordControllerQualityFixMutation(guard *qualityMutationGuard, report harnesslint.Report) error {
	after, err := controller.CaptureWorkspaceSnapshot(w.config.RepoRoot)
	if err != nil {
		return err
	}
	if err := verifyQualityFixMutationProvenance(guard.before, after, report); err != nil {
		return err
	}
	provenance, err := w.persistQualityFixEvidence(report)
	if err != nil {
		return err
	}
	if err := guard.store.PreserveQualityMutationOutput(guard.admission, after, provenance); err != nil {
		return fmt.Errorf("machine quality fixer provenance was not preserved on the controller: %w", err)
	}
	if _, err := guard.store.RecordAdmittedMutation(guard.admission, controller.QualityMutationCommand, "success", after); err != nil {
		return fmt.Errorf(
			"machine quality fixer postimage was applied but the live lease mutation record failed; the preserved provenance stays in state %s and on the staged controller intent: %w",
			qualityFixEvidenceStateKey, err,
		)
	}
	if err := guard.store.ClearQualityMutationIntent(guard.admission); err != nil {
		return err
	}
	return w.syncWorkerEndSnapshotToQualityFix(report.FixEvidence)
}

func (w *Workflow) verifyQualityGateNoFix(guard *qualityMutationGuard, before state.GitSnapshot) error {
	if err := w.qualityGateNoFixDrift(guard, before); err != nil {
		return err
	}
	if guard != nil {
		if err := guard.store.ClearQualityMutationIntent(guard.admission); err != nil {
			return err
		}
	}
	return nil
}

func (w *Workflow) qualityGateNoFixDrift(guard *qualityMutationGuard, before state.GitSnapshot) error {
	if guard != nil {
		after, err := controller.CaptureWorkspaceSnapshot(w.config.RepoRoot)
		if err != nil {
			return err
		}
		if after.Head != guard.before.Head || after.IndexDigest != guard.before.IndexDigest ||
			after.WorktreeDigest != guard.before.WorktreeDigest || after.RefDigest != guard.before.RefDigest {
			return fmt.Errorf(
				"machine quality gate changed the workspace without reporting fixes (fixed=0): head=%s worktree-drift=%t ref-drift=%t",
				after.Head,
				after.WorktreeDigest != guard.before.WorktreeDigest,
				after.RefDigest != guard.before.RefDigest,
			)
		}
		return nil
	}
	after, err := w.captureSnapshot(w.config.RepoRoot)
	if err != nil {
		return err
	}
	if state.EqualGitSnapshot(before, after) {
		return nil
	}
	return fmt.Errorf("machine quality gate changed the workspace without reporting fixes (fixed=0): head=%s", after.Head)
}

func verifyQualityFixMutationProvenance(before, after controller.WorkspaceSnapshot, report harnesslint.Report) error {
	evidence := report.FixEvidence
	if evidence == nil || evidence.Method != harnesslint.FixProvenanceIsolatedPostimageV1 {
		return fmt.Errorf("machine quality fixer provenance is missing (fixed=%d)", report.Fixed)
	}
	if !qualityFixEvidenceMatches(workspaceRuntimeSnapshot(before), evidence.Input) ||
		!qualityFixEvidenceMatches(workspaceRuntimeSnapshot(after), evidence.Output) {
		return fmt.Errorf("machine quality fixer provenance does not match the admitted workspace snapshots (fixed=%d)", report.Fixed)
	}
	if after.Head != before.Head || after.IndexDigest != before.IndexDigest || after.RefDigest != before.RefDigest {
		return fmt.Errorf("machine quality fixer changed workspace authority outside the worktree (fixed=%d)", report.Fixed)
	}
	return nil
}

func workspaceRuntimeSnapshot(snapshot controller.WorkspaceSnapshot) state.GitSnapshot {
	return state.GitSnapshot{Head: snapshot.Head, IndexDigest: snapshot.IndexDigest, WorktreeDigest: snapshot.WorktreeDigest}
}

func (w *Workflow) persistQualityFixEvidence(report harnesslint.Report) (string, error) {
	if report.FixEvidence == nil {
		return "", fmt.Errorf("machine quality fixer reported fixed=%d without provenance evidence", report.Fixed)
	}
	data, err := json.Marshal(report.FixEvidence)
	if err != nil {
		return "", fmt.Errorf("encode machine quality fixer evidence: %w", err)
	}
	if err := w.state.Write(qualityFixEvidenceStateKey, string(data)); err != nil {
		return "", err
	}
	return string(data), nil
}

func (w *Workflow) syncWorkerEndSnapshotToQualityFix(evidence *harnesslint.FixEvidence) error {
	if evidence == nil || evidence.Input == nil || evidence.Output == nil {
		return nil
	}
	saved, err := w.state.LoadWorkerEndSnapshot()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("load worker-end snapshot for machine quality fixer sync: %w", err)
	}
	if !qualityFixEvidenceMatches(saved, evidence.Input) {
		return nil
	}
	return w.state.SaveWorkerEndSnapshot(state.GitSnapshot{
		Head:           evidence.Output.Head,
		IndexDigest:    evidence.Output.IndexDigest,
		WorktreeDigest: evidence.Output.WorktreeDigest,
	})
}
