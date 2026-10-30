package controller

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type ParentCompletionOperation struct {
	Surface           string                      `json:"surface"`
	Operation         string                      `json:"operation"`
	Mode              string                      `json:"mode,omitempty"`
	Command           *PublicationParentCommand   `json:"command,omitempty"`
	Execution         *CompletionExecutionCommand `json:"execution,omitempty"`
	AllowedOperations []string                    `json:"allowed_operations,omitempty"`
	SemanticSlots     []string                    `json:"semantic_slots,omitempty"`
	FinalValidation   *FinalValidationRequest     `json:"final_validation,omitempty"`
	SmokeProbeIDs     []string                    `json:"smoke_probe_ids,omitempty"`
	Reason            string                      `json:"reason,omitempty"`
}

type PublicationParentCommand struct {
	Action              string                         `json:"action"`
	ExpectedGeneration  uint64                         `json:"expected_generation"`
	SourceOID           string                         `json:"source_oid,omitempty"`
	CandidateID         string                         `json:"candidate_id,omitempty"`
	TransitionID        string                         `json:"transition_id,omitempty"`
	Policy              *PublicationPolicy             `json:"policy,omitempty"`
	Evidence            []EvidenceObjectRef            `json:"evidence,omitempty"`
	EvidenceRecord      *CandidateEvidence             `json:"evidence_record,omitempty"`
	FinalValidation     *FinalValidationRequest        `json:"final_validation,omitempty"`
	RequiredValidations *RequiredValidationDeclaration `json:"required_validations,omitempty"`
	FailureDisposition  string                         `json:"failure_disposition,omitempty"`
}

type CompletionExecutionCommand struct {
	Action             string             `json:"action"`
	ExpectedGeneration uint64             `json:"expected_generation"`
	TransitionID       string             `json:"transition_id,omitempty"`
	SuspensionID       string             `json:"suspension_id,omitempty"`
	WorkspaceID        string             `json:"workspace_id,omitempty"`
	SealRef            *EvidenceObjectRef `json:"seal_ref,omitempty"`
}

const (
	PublicationOperationSurface = "controller-publication"
	FinalValidationSurface      = "finalize-check"
	ExecutionOperationSurface   = "controller-execution"
	SuccessorStartSurface       = "start"

	PublicationOperationObserved              = "observed"
	PublicationOperationReopen                = "reopen-unsealed"
	PublicationOperationNone                  = "none"
	PublicationOperationRecover               = "recover"
	PublicationOperationAdopt                 = "adopt"
	PublicationOperationAdoptSource           = "adopt-source"
	PublicationOperationAccept                = "complete"
	PublicationOperationParentOnlyMaterialize = "materialize-parent-only"
	PublicationOperationFinalValidation       = "final-validation"
	PublicationOperationPromote               = "promote"
	PublicationOperationPublish               = "publish"
	PublicationOperationSuccessorStart        = "successor-start"
	PublicationOperationValidationFailure     = "resolve-validation-failure"
	PublicationOperationInstall               = "install-completion"
	PublicationOperationPostInstallSmoke      = "post-install-smoke"

	ParentCompletionModeParentOnly = "parent-only"

	CompletionSlotMessage             = "message"
	CompletionSlotRequireInstall      = "require_install"
	CompletionSlotFailureDisposition  = "failure_disposition"
	CompletionSlotRequiredValidations = "required_validations"
)

func noneCompletionOperation(reason string) ParentCompletionOperation {
	return ParentCompletionOperation{Surface: "", Operation: PublicationOperationNone, Reason: reason}
}

func (s *Store) ProjectParentCompletion() (ParentCompletionOperation, error) {
	head, err := s.LoadHead()
	if err != nil {
		return ParentCompletionOperation{}, err
	}
	if head.Status != ControllerStatusActive {
		return noneCompletionOperation("repository controller is fail-closed"), nil
	}
	if head.PendingTransitionID != "" {
		return s.projectTransitionRecovery(head)
	}
	return s.projectActiveCompletion(head)
}

func (s *Store) projectActiveCompletion(head RepositoryControllerHead) (ParentCompletionOperation, error) {
	if operation, ok, err := s.projectExternalAdvancement(head); err != nil || ok {
		return operation, err
	}
	awaiting, err := s.liveRuntimeAwaitingCompletion(head)
	if err != nil {
		return ParentCompletionOperation{}, err
	}
	if awaiting {
		return s.projectAcceptanceOperation(head)
	}
	if head.AcceptedCandidateRef != nil {
		return s.projectAcceptedCandidateOperation(head)
	}
	if operation, selected, err := s.projectParentOnlyCompletionOperation(head); err != nil || selected {
		return operation, err
	}
	if operation, selected, err := s.projectCompletionSuccessor(head); selected || err != nil {
		return operation, err
	}
	return s.projectAcceptanceOperation(head)
}

func (s *Store) projectTransitionRecovery(head RepositoryControllerHead) (ParentCompletionOperation, error) {
	pending, err := s.PendingAdvancementRecovery(head)
	if err != nil {
		return ParentCompletionOperation{}, err
	}
	if !pending {
		return ParentCompletionOperation{Surface: ExecutionOperationSurface, Operation: PublicationOperationRecover, Execution: &CompletionExecutionCommand{Action: "recover", ExpectedGeneration: head.ControllerGeneration, TransitionID: head.PendingTransitionID}}, nil
	}
	return ParentCompletionOperation{
		Surface:   PublicationOperationSurface,
		Operation: PublicationOperationRecover,
		Command: &PublicationParentCommand{
			Action:             PublicationOperationRecover,
			ExpectedGeneration: head.ControllerGeneration,
			TransitionID:       head.PendingTransitionID,
		},
	}, nil
}

func (s *Store) projectExternalAdvancement(head RepositoryControllerHead) (ParentCompletionOperation, bool, error) {
	if !retiredMetadataHead(head) || head.PublicationPolicy == nil {
		return ParentCompletionOperation{}, false, nil
	}
	authority, err := ResolveCommittedTaskAuthority(s.identity.PrimaryRoot)
	if err != nil || authority.Snapshot.HeadOID == head.IntegrationTip {
		return ParentCompletionOperation{}, false, err
	}
	action := PublicationOperationAdopt
	requiresSource, err := s.RetiredSourceAdvancementRequired(head, authority.Snapshot.HeadOID)
	if err != nil {
		return ParentCompletionOperation{}, false, err
	}
	if requiresSource {
		action = PublicationOperationAdoptSource
	}
	policy := *head.PublicationPolicy
	return ParentCompletionOperation{
		Surface:   PublicationOperationSurface,
		Operation: action,
		Command: &PublicationParentCommand{
			Action:             action,
			ExpectedGeneration: head.ControllerGeneration,
			SourceOID:          authority.Snapshot.HeadOID,
			Policy:             &policy,
		},
	}, true, nil
}

func (s *Store) projectAcceptedCandidateOperation(head RepositoryControllerHead) (ParentCompletionOperation, error) {
	candidate, err := s.LoadAcceptedCandidate(*head.AcceptedCandidateRef)
	if err != nil {
		return ParentCompletionOperation{}, err
	}
	if (candidate.State == candidatePrepared || candidate.State == candidatePromoted) && !candidate.EvidenceValid {
		return s.projectCompletionMaterialization(head, candidate)
	}
	switch candidate.State {
	case candidatePrepared:
		return s.projectPreparedCandidateOperation(head, candidate)
	case candidatePromoted:
		return s.projectCandidateOperation(head, candidate, PublicationOperationPublish), nil
	default:
		return ParentCompletionOperation{Operation: PublicationOperationObserved}, nil
	}
}

func (s *Store) projectPreparedCandidateOperation(head RepositoryControllerHead, candidate AcceptedCandidate) (ParentCompletionOperation, error) {
	failed, err := s.failedFinalValidation(candidate)
	if err != nil {
		return ParentCompletionOperation{}, err
	}
	if failed != nil {
		operation := s.projectCandidateOperation(head, candidate, PublicationOperationValidationFailure)
		operation.Command.SourceOID = candidate.CommitOID
		operation.Command.FinalValidation = failed
		operation.SemanticSlots = []string{CompletionSlotFailureDisposition}
		return operation, nil
	}
	outstanding, err := s.outstandingFinalValidations(candidate)
	if err != nil {
		return ParentCompletionOperation{}, err
	}
	if len(outstanding) != 0 {
		return ParentCompletionOperation{
			Surface:         FinalValidationSurface,
			Operation:       PublicationOperationFinalValidation,
			FinalValidation: &outstanding[0],
		}, nil
	}
	if err := s.requireCompletedCandidateInstall(candidate); err != nil {
		if !errors.Is(err, errCandidateInstallPending) {
			return ParentCompletionOperation{}, err
		}
		operation := s.projectCandidateOperation(head, candidate, PublicationOperationInstall)
		operation.SemanticSlots = []string{CompletionSlotFailureDisposition}
		return operation, nil
	}
	probes, err := s.outstandingSmokeProbes(candidate)
	if err != nil {
		return ParentCompletionOperation{}, err
	}
	if len(probes) != 0 {
		operation := s.projectCandidateOperation(head, candidate, PublicationOperationPostInstallSmoke)
		operation.SmokeProbeIDs = smokeProbeIDs(probes)
		return operation, nil
	}
	return s.projectCandidateOperation(head, candidate, PublicationOperationPromote), nil
}

func smokeProbeIDs(probes []SmokeProbeDeclaration) []string {
	ids := make([]string, 0, len(probes))
	for _, probe := range probes {
		ids = append(ids, probe.ProbeID)
	}
	return ids
}

func (*Store) projectCandidateOperation(head RepositoryControllerHead, candidate AcceptedCandidate, action string) ParentCompletionOperation {
	return ParentCompletionOperation{
		Surface:   PublicationOperationSurface,
		Operation: action,
		Command: &PublicationParentCommand{
			Action:             action,
			ExpectedGeneration: head.ControllerGeneration,
			CandidateID:        candidate.CandidateID,
			SourceOID:          candidate.CommitOID,
		},
	}
}

func (s *Store) projectParentOnlyCompletionOperation(head RepositoryControllerHead) (ParentCompletionOperation, bool, error) {
	if head.LiveAttemptID != "" || head.LiveLeaseID != "" || head.ExecutionTaskRef != nil {
		return s.projectLiveParentOnlyCompletion(head)
	}
	if head.AcceptedCandidateRef != nil {
		return ParentCompletionOperation{}, false, nil
	}
	return s.projectQuiescentParentOnlyMaterialization(head)
}

func (s *Store) projectLiveParentOnlyCompletion(head RepositoryControllerHead) (ParentCompletionOperation, bool, error) {
	if head.LiveAttemptID == "" || head.LiveLeaseID == "" || head.ExecutionTaskRef == nil {
		return ParentCompletionOperation{}, false, nil
	}
	attempt, live, err := s.parentOnlyLiveExecution(head)
	if err != nil || !live {
		return ParentCompletionOperation{}, false, err
	}
	runtime, _, err := s.boundAttemptRuntime(Admission{Attempt: attempt})
	if err != nil {
		return ParentCompletionOperation{}, false, err
	}
	if runtime != nil {
		return ParentCompletionOperation{}, false, fmt.Errorf("parent-only execution %s must not carry a Runtime", attempt.AttemptID)
	}
	policy, err := s.parentCompletionPolicy(head)
	if err != nil {
		return ParentCompletionOperation{}, false, err
	}
	return ParentCompletionOperation{Surface: PublicationOperationSurface, Mode: ParentCompletionModeParentOnly, Operation: PublicationOperationAccept,
		Command:           &PublicationParentCommand{Action: PublicationOperationAccept, ExpectedGeneration: head.ControllerGeneration, Policy: &policy},
		SemanticSlots:     []string{CompletionSlotMessage, CompletionSlotRequireInstall, CompletionSlotRequiredValidations},
		AllowedOperations: []string{PublicationOperationAccept}}, true, nil
}

func (s *Store) projectQuiescentParentOnlyMaterialization(head RepositoryControllerHead) (ParentCompletionOperation, bool, error) {
	if head.ObservedRemoteTip != "" && head.ObservedRemoteTip != head.IntegrationTip {
		return ParentCompletionOperation{}, false, nil
	}
	quiescentRetired := retiredMetadataHead(head) || (head.UserTerminationRef != nil && head.MetadataLineageRef == nil)
	if !IsPristine(head) && !quiescentRetired {
		return ParentCompletionOperation{}, false, nil
	}
	authority, err := s.parentOnlySuccessorAuthority(head)
	if err != nil {
		if IsPristine(head) {
			return ParentCompletionOperation{}, false, nil
		}
		return ParentCompletionOperation{}, false, err
	}
	if authority.ExecutionMode != ExecutionAuthorityParentOnly {
		return ParentCompletionOperation{}, false, nil
	}
	return ParentCompletionOperation{Surface: ExecutionOperationSurface, Mode: ParentCompletionModeParentOnly, Operation: PublicationOperationParentOnlyMaterialize,
		Execution: &CompletionExecutionCommand{Action: PublicationOperationParentOnlyMaterialize, ExpectedGeneration: head.ControllerGeneration}}, true, nil
}

func (s *Store) projectAcceptanceOperation(head RepositoryControllerHead) (ParentCompletionOperation, error) {
	if head.LiveAttemptID == "" || head.LiveLeaseID == "" || head.ExecutionTaskRef == nil {
		return noneCompletionOperation("controller has no live execution awaiting completion"), nil
	}
	awaiting, err := s.runtimeAwaitingCompletion(head)
	if err != nil || !awaiting {
		return noneCompletionOperation("live execution runtime has not reached parent completion"), err
	}
	admission, err := s.liveCompletionAdmission(head)
	if err != nil {
		return ParentCompletionOperation{}, err
	}
	reopen, err := s.completionRequiresUnsealedReopen(admission)
	if err != nil {
		return ParentCompletionOperation{}, err
	}
	if reopen {
		return s.projectUnsealedReopen(head.ControllerGeneration), nil
	}
	policy, err := s.parentCompletionPolicy(head)
	if err != nil {
		return ParentCompletionOperation{}, err
	}
	return ParentCompletionOperation{Surface: PublicationOperationSurface, Operation: PublicationOperationAccept,
		Command:       &PublicationParentCommand{Action: PublicationOperationAccept, ExpectedGeneration: head.ControllerGeneration, Policy: &policy},
		SemanticSlots: []string{CompletionSlotMessage, CompletionSlotRequireInstall, CompletionSlotRequiredValidations}, AllowedOperations: []string{PublicationOperationAccept, PublicationOperationReopen}}, nil
}

func (*Store) projectUnsealedReopen(generation uint64) ParentCompletionOperation {
	return ParentCompletionOperation{Surface: PublicationOperationSurface, Operation: PublicationOperationReopen, Command: &PublicationParentCommand{Action: PublicationOperationReopen, ExpectedGeneration: generation}, SemanticSlots: []string{CompletionSlotMessage}}
}

func (s *Store) ProjectParentCompletionOperation(requested string) (ParentCompletionOperation, error) {
	operation, err := s.ProjectParentCompletion()
	if err != nil || requested == "" || requested == operation.Operation {
		return operation, err
	}
	if requested == PublicationOperationReopen && operation.Operation == PublicationOperationAccept {
		return s.projectUnsealedReopen(operation.Command.ExpectedGeneration), nil
	}
	return ParentCompletionOperation{}, fmt.Errorf("completion operation %s is not admitted at %s", requested, operation.Operation)
}

func (s *Store) runtimeAwaitingCompletion(head RepositoryControllerHead) (bool, error) {
	lease, err := s.loadLease(head.LiveLeaseID)
	if err != nil {
		return false, err
	}
	root := s.projectWorkspaceRoot(lease.WorkspaceID)
	if root == "" {
		return false, fmt.Errorf("live completion workspace is unavailable")
	}
	cfg := WorkflowTaskConfig(s.config, s.identity, *head.ExecutionTaskRef)
	cfg.RepoRoot = root
	return state.AttachStateStore(cfg).TaskStatus() == state.TaskStatusAwaitingParentCompletion, nil
}

func (s *Store) liveCompletionAdmission(head RepositoryControllerHead) (Admission, error) {
	authority, err := MutationAuthorityFromHead(head)
	if err != nil {
		return Admission{}, err
	}
	lease, err := s.loadLease(head.LiveLeaseID)
	if err != nil {
		return Admission{}, err
	}
	root := s.projectWorkspaceRoot(lease.WorkspaceID)
	if root == "" {
		return Admission{}, fmt.Errorf("live completion workspace is unavailable")
	}
	workspace, err := ResolveWorkspaceIdentity(root, s.identity)
	if err != nil {
		return Admission{}, err
	}
	snapshot, err := CaptureWorkspaceSnapshot(root)
	if err != nil {
		return Admission{}, err
	}
	return s.AdmitMutation(authority, workspace, snapshot)
}

func (s *Store) parentCompletionPolicy(head RepositoryControllerHead) (PublicationPolicy, error) {
	if head.PublicationPolicy != nil {
		return *head.PublicationPolicy, nil
	}
	policy, err := deriveRepositoryPublicationPolicy(s.identity.PrimaryRoot)
	if err != nil {
		return PublicationPolicy{}, fmt.Errorf("publication policy is not recorded and cannot be derived: %w", err)
	}
	return policy, nil
}

func deriveRepositoryPublicationPolicy(repo string) (PublicationPolicy, error) {
	remotes, err := gitTrimmedLines(repo, "remote")
	if err != nil {
		return PublicationPolicy{}, err
	}
	branch, err := gitTrimmed(repo, "symbolic-ref", "--short", "HEAD")
	if err != nil || branch == "" {
		return PublicationPolicy{}, fmt.Errorf("publication requires a local branch HEAD: %w", err)
	}
	mergeRef, err := gitTrimmed(repo, "config", "--get", "branch."+branch+".merge")
	if err != nil || !strings.HasPrefix(mergeRef, "refs/heads/") {
		return PublicationPolicy{}, fmt.Errorf("local branch %s has no upstream branch: %w", branch, err)
	}
	boundRemote, err := gitTrimmed(repo, "config", "--get", "branch."+branch+".remote")
	if err != nil {
		return PublicationPolicy{}, fmt.Errorf("local branch %s has no upstream remote: %w", branch, err)
	}
	if !slices.Contains(remotes, boundRemote) {
		return PublicationPolicy{}, fmt.Errorf("local branch %s is not bound to a configured remote", branch)
	}
	policy := PublicationPolicy{Remote: boundRemote, LocalRef: "refs/heads/" + branch, RemoteRef: mergeRef}
	if err := validatePublicationPolicy(repo, policy); err != nil {
		return PublicationPolicy{}, err
	}
	return policy, nil
}

func (s *Store) outstandingFinalValidations(c AcceptedCandidate) ([]FinalValidationRequest, error) {
	requests, err := normalizeRequiredValidations(c.RequiredValidations)
	if err != nil {
		return nil, err
	}
	if len(requests) == 0 {
		return nil, nil
	}
	source, err := s.finalValidationSource(c)
	if err != nil {
		return nil, err
	}
	return outstandingObligations(requests, func(request FinalValidationRequest) (bool, error) {
		return s.finalValidationSatisfied(c, request, source)
	})
}

func (s *Store) finalValidationSatisfied(c AcceptedCandidate, request FinalValidationRequest, source finalValidationSource) (bool, error) {
	repoRoot, err := s.finalValidationWorkspace(c, request)
	if err != nil {
		return false, err
	}
	module, err := s.finalValidationRunStore(c.TaskRef)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	record, ok, err := latestFinalValidationRun(module, repoRoot, request.Form)
	if err != nil {
		return false, err
	}
	if !ok || !finalValidationMatchesSource(record, repoRoot, source, request) {
		return false, nil
	}
	return qualitygate.VerifyTerminalPass(record) == nil, nil
}

func gitTrimmedLines(repo string, args ...string) ([]string, error) {
	output, err := runGitBinary(repo, nil, args...)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	trimmed := make([]string, 0, len(lines))
	for _, line := range lines {
		if line != "" {
			trimmed = append(trimmed, line)
		}
	}
	return trimmed, nil
}

func (s *Store) liveRuntimeAwaitingCompletion(head RepositoryControllerHead) (bool, error) {
	if head.LiveLeaseID == "" || head.ExecutionTaskRef == nil {
		return false, nil
	}
	return s.runtimeAwaitingCompletion(head)
}

func (s *Store) projectCompletionSuccessor(head RepositoryControllerHead) (ParentCompletionOperation, bool, error) {
	if head.ExecutionTaskRef != nil || head.LiveLeaseID != "" || head.MetadataLineageRef == nil || head.IntegrationTip != head.ObservedRemoteTip {
		return ParentCompletionOperation{}, false, nil
	}
	if _, err := s.QuiescentRuntimeAdmission(head); err != nil {
		return ParentCompletionOperation{}, true, err
	}
	return ParentCompletionOperation{Surface: SuccessorStartSurface, Operation: PublicationOperationSuccessorStart}, true, nil
}

func (s *Store) completionRequiresUnsealedReopen(admission Admission) (bool, error) {
	runtime, _, err := s.boundAttemptRuntime(admission)
	if err != nil {
		return false, err
	}
	if runtime == nil {
		return false, fmt.Errorf("completion has no bound Runtime")
	}
	if err := validateCompletionRuntimeAuthority(runtime); err != nil {
		return false, err
	}
	outcome, err := runtime.CurrentParentCompletionOutcome()
	if err != nil {
		return false, err
	}
	if outcome == nil || outcome.Terminal != state.SessionRotationTerminalAccept {
		return false, fmt.Errorf("completion has no accepted Runtime outcome")
	}
	return outcome.Source == nil, nil
}

func validateCompletionRuntimeAuthority(runtime *state.StateStore) error {
	plan, err := runtime.ParentActionPlan()
	if err != nil {
		return err
	}
	if plan.RequiredAction == state.ParentActionReopen {
		return fmt.Errorf("runtime-only publication candidate cannot be completed by Controller candidate authority")
	}
	if !plan.Allows(state.ParentActionComplete) {
		return fmt.Errorf("runtime does not admit candidate completion")
	}
	_, candidateErr := runtime.LoadPublicationCandidate()
	if candidateErr == nil {
		return fmt.Errorf("controller completion requires no Runtime-only publication candidate")
	}
	if !errors.Is(candidateErr, os.ErrNotExist) {
		return candidateErr
	}
	return nil
}
