package controller

import (
	"fmt"
	"reflect"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type CompletionReadiness struct {
	SchemaVersion          int                            `json:"schema_version"`
	SuccessorRuntimeTaskID string                         `json:"successor_runtime_task_id"`
	LintRequired           bool                           `json:"lint_required"`
	LintRef                *EvidenceObjectRef             `json:"lint_ref,omitempty"`
	RuntimeTaskID          string                         `json:"runtime_task_id,omitempty"`
	RuntimeCompletion      *state.ParentCompletionOutcome `json:"runtime_completion,omitempty"`
	OperationDigest        string                         `json:"operation_digest"`
	FinalOID               string                         `json:"final_oid"`
}

type CompletionLintEvidence struct {
	SchemaVersion   int                `json:"schema_version"`
	OperationDigest string             `json:"operation_digest"`
	FinalOID        string             `json:"final_oid"`
	Report          harnesslint.Report `json:"report"`
}

func (s *Store) prepareCompletionReadiness(op ExecutionOperation) (EvidenceObjectRef, error) {
	if err := s.verifyCompletionLocalTarget(op); err != nil {
		return EvidenceObjectRef{}, err
	}
	runtimeTaskID, outcome, err := s.prepareCompletionRuntime(op)
	if err != nil {
		return EvidenceObjectRef{}, err
	}
	lintRef, err := s.completionMetadataLint(op)
	if err != nil {
		return EvidenceObjectRef{}, err
	}
	if _, err := s.planEvidencePublication(op.Source.Head, op.Transition, op.Evidence); err != nil {
		return EvidenceObjectRef{}, err
	}
	target := op.Source.Head
	applyCompletionTargetAuthority(&target, *op.Terminal)
	successorID, err := s.QuiescentRuntimeAdmission(target)
	if err != nil {
		return EvidenceObjectRef{}, err
	}
	ready := CompletionReadiness{SchemaVersion: controllerSchemaVersion, SuccessorRuntimeTaskID: successorID, LintRequired: lintRef != nil, LintRef: lintRef, RuntimeTaskID: runtimeTaskID, RuntimeCompletion: outcome, OperationDigest: op.Transition.OperationDigest, FinalOID: op.Terminal.Record.CandidateOID}
	return s.putEvidenceJSON("completion-readiness", op.Transition.TransitionID, ready)
}

func (s *Store) completionMetadataLint(op ExecutionOperation) (*EvidenceObjectRef, error) {
	applies, err := repositoryharness.QualityToolsApply(s.identity.PrimaryRoot)
	if err != nil || !applies {
		return nil, err
	}
	report, err := harnesslint.RunCommit(s.identity.PrimaryRoot, op.Terminal.Record.CandidateOID)
	if err != nil {
		return nil, err
	}
	if report.Status != candidateEvidencePass || len(report.Violations) != 0 {
		var locators []string
		for _, violation := range report.Violations {
			locators = append(locators, fmt.Sprintf("%s:%s:%d:%s", violation.Rule, violation.Path, violation.Line, violation.Message))
		}
		ref, storeErr := s.putEvidenceJSON("completion-lint", op.Transition.TransitionID, CompletionLintEvidence{SchemaVersion: controllerSchemaVersion, OperationDigest: op.Transition.OperationDigest, FinalOID: op.Terminal.Record.CandidateOID, Report: report})
		if storeErr != nil {
			return nil, storeErr
		}
		return nil, fmt.Errorf("completion final Metadata lint failed with %d violations; evidence=%s; %v", len(report.Violations), ref.Digest, locators)
	}
	ref, err := s.putEvidenceJSON("completion-lint", op.Transition.TransitionID, CompletionLintEvidence{SchemaVersion: controllerSchemaVersion, OperationDigest: op.Transition.OperationDigest, FinalOID: op.Terminal.Record.CandidateOID, Report: report})
	return &ref, err
}

func (s *Store) verifyCompletionLocalTarget(op ExecutionOperation) error {
	if err := s.validateTerminalMetadataOperation(op); err != nil {
		return err
	}
	actual, err := s.observeTerminalFileEffects(op)
	if err != nil {
		return err
	}
	for _, effect := range terminalMetadataEffects(*op.Terminal, "")[1:] {
		if actual[effect.Key()] != effect.ExpectedNew {
			return fmt.Errorf("completion final metadata differs: %s", effect.Resource)
		}
	}
	c, err := s.LoadAcceptedCandidate(op.Terminal.Record.CandidateRef)
	if err != nil {
		return err
	}
	if err := s.validateCandidatePublicationLocal(c); err != nil {
		return err
	}
	if err := s.verifyPrimaryPublicationTree(c); err != nil {
		return err
	}
	authority, err := ResolveCommittedTaskAuthority(s.identity.PrimaryRoot)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(authority.Snapshot, op.Terminal.Record.ResultProject) || !authority.Task.Equal(*op.Terminal.RootTaskRef) {
		return fmt.Errorf("completion final commit and target authority differ")
	}
	target := op.Source.Head
	applyCompletionTargetAuthority(&target, *op.Terminal)
	if err := validateCommittedTerminalHead(*op.Terminal, target); err != nil {
		return err
	}
	return validateCompletionSuccessorAuthority(target, authority)
}

func (s *Store) validateCompletionReadiness(op ExecutionOperation, phase TransitionState) (CompletionReadiness, error) {
	ready, err := s.loadCompletionReadiness(op, phase)
	if err != nil {
		return ready, err
	}
	taskID, outcome, err := s.prepareCompletionRuntime(op)
	if err != nil {
		return ready, err
	}
	target := op.Source.Head
	applyCompletionTargetAuthority(&target, *op.Terminal)
	successorID, err := s.QuiescentRuntimeAdmission(target)
	if err != nil {
		return ready, err
	}
	if taskID != ready.RuntimeTaskID || !reflect.DeepEqual(outcome, ready.RuntimeCompletion) || successorID != ready.SuccessorRuntimeTaskID {
		return ready, fmt.Errorf("completion Runtime identity changed after readiness")
	}
	applies, err := repositoryharness.QualityToolsApply(s.identity.PrimaryRoot)
	if err != nil {
		return ready, err
	}
	if applies != ready.LintRequired {
		return ready, fmt.Errorf("completion lint scope changed after readiness")
	}
	if _, err := s.planEvidencePublication(op.Source.Head, op.Transition, op.Evidence); err != nil {
		return ready, err
	}
	return ready, s.verifyCompletionLocalTarget(op)
}

func (s *Store) loadCompletionReadiness(op ExecutionOperation, phase TransitionState) (CompletionReadiness, error) {
	var ready CompletionReadiness
	if phase.Completion == nil || phase.Completion.ReadinessRef == nil {
		return ready, fmt.Errorf("publication requires completion readiness")
	}
	ref := *phase.Completion.ReadinessRef
	if err := validateTypedEvidenceRef(ref, "completion-readiness"); err != nil {
		return ready, err
	}
	data, err := s.LoadEvidenceObject(ref)
	if err != nil {
		return ready, err
	}
	if err := decodeControllerJSON(data, &ready); err != nil {
		return ready, err
	}
	if ready.SchemaVersion != controllerSchemaVersion || ref.LogicalIdentity != op.Transition.TransitionID || ready.OperationDigest != op.Transition.OperationDigest || ready.FinalOID != op.Terminal.Record.CandidateOID {
		return ready, fmt.Errorf("completion readiness belongs to different operation")
	}
	if err := validateCompletionRuntimePlan(ready); err != nil {
		return ready, err
	}
	return ready, s.validateCompletionLint(op, ready)
}

func (s *Store) validateCompletionLint(op ExecutionOperation, ready CompletionReadiness) error {
	if !ready.LintRequired && ready.LintRef == nil {
		return nil
	}
	if !ready.LintRequired || ready.LintRef == nil {
		return fmt.Errorf("completion final Metadata lint evidence is missing")
	}
	ref, evidence, err := s.loadCompletionLint(*ready.LintRef)
	if err != nil {
		return err
	}
	if evidence.SchemaVersion != controllerSchemaVersion || ref.LogicalIdentity != op.Transition.TransitionID || evidence.OperationDigest != op.Transition.OperationDigest || evidence.FinalOID != ready.FinalOID || evidence.Report.Status != candidateEvidencePass || len(evidence.Report.Violations) != 0 {
		return fmt.Errorf("completion final Metadata lint evidence is stale or failed")
	}
	return nil
}

func (s *Store) loadCompletionLint(ref EvidenceObjectRef) (EvidenceObjectRef, CompletionLintEvidence, error) {
	var evidence CompletionLintEvidence
	if err := validateTypedEvidenceRef(ref, "completion-lint"); err != nil {
		return ref, evidence, err
	}
	data, err := s.LoadEvidenceObject(ref)
	if err != nil {
		return ref, evidence, err
	}
	err = decodeControllerJSON(data, &evidence)
	return ref, evidence, err
}

func validateCompletionSuccessorAuthority(head RepositoryControllerHead, authority CommittedTaskAuthority) error {
	if head.RootTaskRef == nil || !head.RootTaskRef.Equal(authority.Task) || head.ExecutionTaskRef != nil || head.AcceptedCandidateRef != nil || head.LiveAttemptID != "" || head.LiveLeaseID != "" || head.ProjectSnapshotID != authority.ProjectSnapshotID || head.IntegrationTip != authority.Snapshot.HeadOID {
		return fmt.Errorf("completion successor bootstrap authority differs")
	}
	return nil
}
