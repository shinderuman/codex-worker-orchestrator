package controller

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type FindingProofClass string

type FindingEvidenceKind string

type FindingDecisionKind string

type FindingDispositionKind string

type FindingIntentKind string

type FindingEvidenceRef struct {
	Kind FindingEvidenceKind `json:"kind"`
	ID   string              `json:"id"`
}

type FindingObservationInput struct {
	Producer   string               `json:"producer"`
	ProofClass FindingProofClass    `json:"proof_class"`
	Evidence   []FindingEvidenceRef `json:"evidence,omitempty"`
	ProblemKey string               `json:"problem_key"`
}

type FindingRecord struct {
	SchemaVersion              int                  `json:"schema_version"`
	FindingID                  string               `json:"finding_id"`
	RepositoryIdentity         string               `json:"repository_identity"`
	ProjectSnapshotID          string               `json:"project_snapshot_id"`
	SourceAttemptID            string               `json:"source_attempt_id"`
	SourceSemanticTaskRef      SemanticTaskRef      `json:"source_semantic_task_ref"`
	SourceWorkspaceSnapshotID  string               `json:"source_workspace_snapshot_id"`
	SourceControllerGeneration uint64               `json:"source_controller_generation"`
	Producer                   string               `json:"producer"`
	ProofClass                 FindingProofClass    `json:"proof_class"`
	Evidence                   []FindingEvidenceRef `json:"evidence,omitempty"`
	ProblemKey                 string               `json:"problem_key"`
	CanonicalFindingID         string               `json:"canonical_finding_id"`
	ObservedAt                 time.Time            `json:"observed_at"`
}

type FindingDecision struct {
	Kind             FindingDecisionKind `json:"kind"`
	TargetTaskRef    *SemanticTaskRef     `json:"target_task_ref,omitempty"`
	BlockingBoundary string               `json:"blocking_boundary,omitempty"`
}

type FindingDisposition struct {
	SchemaVersion        int                    `json:"schema_version"`
	DispositionID        string                 `json:"disposition_id"`
	FindingID            string                 `json:"finding_id"`
	CanonicalFindingID   string                 `json:"canonical_finding_id"`
	Kind                 FindingDispositionKind `json:"kind"`
	TargetTaskRef        *SemanticTaskRef       `json:"target_task_ref,omitempty"`
	BlockingBoundary     string                 `json:"blocking_boundary,omitempty"`
	ControllerGeneration uint64                 `json:"controller_generation"`
	EpisodeID            string                 `json:"episode_id,omitempty"`
	EpisodeRevision      uint64                 `json:"episode_revision,omitempty"`
	CommittedAt          time.Time              `json:"committed_at"`
}

type FindingDispositionResult struct {
	Finding     FindingRecord           `json:"finding"`
	Disposition *FindingDisposition     `json:"disposition,omitempty"`
	Intent      FindingIntentKind       `json:"intent"`
	Reason      string                  `json:"reason,omitempty"`
	Episode     *BlockerEpisodeRevision `json:"episode,omitempty"`
	NextTaskRef *SemanticTaskRef        `json:"next_task_ref,omitempty"`
}

type findingProblemBinding struct {
	SchemaVersion      int              `json:"schema_version"`
	ProblemKey         string           `json:"problem_key"`
	CanonicalFindingID string           `json:"canonical_finding_id"`
	TargetTaskRef      *SemanticTaskRef `json:"target_task_ref,omitempty"`
	UpdatedAt          time.Time        `json:"updated_at"`
}

const (
	FindingProofUnverified      FindingProofClass = "unverified"
	FindingProofAttemptMutation FindingProofClass = "attempt-mutation"

	FindingEvidenceMutation FindingEvidenceKind = "mutation"

	FindingDecisionAmbiguous              FindingDecisionKind = "ambiguous"
	FindingDecisionSameTask               FindingDecisionKind = "same-task"
	FindingDecisionIndependentNonBlocking FindingDecisionKind = "independent-nonblocking"
	FindingDecisionIndependentBlocking    FindingDecisionKind = "independent-blocking"
	FindingDecisionDuplicate              FindingDecisionKind = "duplicate-finding"
	FindingDecisionNonActionable          FindingDecisionKind = "non-actionable"

	FindingDispositionSameTask               FindingDispositionKind = "same-task"
	FindingDispositionIndependentNonBlocking FindingDispositionKind = "independent-nonblocking"
	FindingDispositionIndependentBlocking    FindingDispositionKind = "independent-blocking"
	FindingDispositionDuplicate              FindingDispositionKind = "duplicate-finding"
	FindingDispositionNonActionable          FindingDispositionKind = "non-actionable"

	FindingIntentAwaitingDisposition  FindingIntentKind = "awaiting-disposition"
	FindingIntentSameTaskCorrection   FindingIntentKind = "same-task-correction"
	FindingIntentRegisterNonBlocking  FindingIntentKind = "register-nonblocking"
	FindingIntentOpenBlockerEpisode   FindingIntentKind = "open-blocker-episode"
	FindingIntentReplanBlockerEpisode FindingIntentKind = "replan-blocker-episode"
	FindingIntentDuplicate            FindingIntentKind = "duplicate-finding"
	FindingIntentNoAction             FindingIntentKind = "no-action"
	FindingIntentNoRunnable           FindingIntentKind = "no-runnable-dependency"
	FindingIntentStartBlockerTask     FindingIntentKind = "start-blocker-task"
	FindingIntentResumeBlockerTask    FindingIntentKind = "resume-blocker-task"
)

func (s *Store) ObserveFinding(admission Admission, input FindingObservationInput) (FindingRecord, error) {
	lock, err := s.acquireMutationLock()
	if err != nil {
		return FindingRecord{}, err
	}
	defer func() { _ = lock.Close() }()

	current, err := s.admitMutation(admission.MutationAuthority(), admission.Workspace, admission.Snapshot)
	if err != nil {
		return FindingRecord{}, fmt.Errorf("admit finding source: %w", err)
	}
	if err := s.validateFindingInput(current, input); err != nil {
		return FindingRecord{}, err
	}
	return s.observeFindingLocked(
		current.Head.ProjectSnapshotID,
		current.Attempt,
		current.Snapshot.ID,
		current.Head.ControllerGeneration,
		input,
	)
}

func (s *Store) ObserveTerminalFinding(
	attemptID string,
	projectSnapshotID string,
	input FindingObservationInput,
) (FindingRecord, error) {
	lock, err := s.acquireMutationLock()
	if err != nil {
		return FindingRecord{}, err
	}
	defer func() { _ = lock.Close() }()

	attempt, err := s.loadAttempt(attemptID)
	if err != nil {
		return FindingRecord{}, err
	}
	if attempt.AttemptState != AttemptStateAccepted {
		return FindingRecord{}, fmt.Errorf("post-completion finding requires an accepted source attempt")
	}
	project, err := s.LoadProjectSnapshot(projectSnapshotID)
	if err != nil {
		return FindingRecord{}, err
	}
	if !projectHasTask(project, attempt.SemanticTaskRef) {
		return FindingRecord{}, fmt.Errorf("accepted source attempt is not bound to the supplied project snapshot")
	}
	if err := validateFindingInputShape(input); err != nil {
		return FindingRecord{}, err
	}
	if input.ProofClass != FindingProofUnverified {
		return FindingRecord{}, fmt.Errorf("post-completion finding proof class %s is not supported", input.ProofClass)
	}
	return s.observeFindingLocked(
		projectSnapshotID,
		attempt,
		attempt.WorkspaceSnapshotID,
		attempt.StartControllerGeneration,
		input,
	)
}

func (s *Store) validateFindingInput(admission Admission, input FindingObservationInput) error {
	if err := validateFindingInputShape(input); err != nil {
		return err
	}
	switch input.ProofClass {
	case FindingProofUnverified:
		return nil
	case FindingProofAttemptMutation:
		if len(input.Evidence) == 0 {
			return fmt.Errorf("attempt-mutation proof requires mutation evidence")
		}
		for _, ref := range input.Evidence {
			if ref.Kind != FindingEvidenceMutation || ref.ID == "" {
				return fmt.Errorf("attempt-mutation proof contains unsupported evidence")
			}
			var mutation MutationRecord
			if err := readJSON(s.mutationPath(ref.ID), &mutation); err != nil {
				return fmt.Errorf("read finding mutation evidence %s: %w", ref.ID, err)
			}
			if mutation.SchemaVersion != controllerSchemaVersion ||
				mutation.MutationID != ref.ID ||
				mutation.AttemptID != admission.Attempt.AttemptID ||
				mutation.TargetGeneration != admission.Head.ControllerGeneration ||
				mutation.TargetLeaseID != admission.Lease.LeaseID {
				return fmt.Errorf("finding mutation evidence %s is not bound to the current execution attempt", ref.ID)
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported finding proof class %s", input.ProofClass)
	}
}

func validateFindingInputShape(input FindingObservationInput) error {
	if input.Producer == "" {
		return fmt.Errorf("finding producer is required")
	}
	if input.ProblemKey == "" {
		return fmt.Errorf("finding problem identity is required")
	}
	if input.ProofClass == "" {
		return fmt.Errorf("finding proof class is required")
	}
	for _, ref := range input.Evidence {
		if ref.Kind == "" || ref.ID == "" {
			return fmt.Errorf("finding evidence reference is incomplete")
		}
	}
	return nil
}

func (s *Store) observeFindingLocked(
	projectSnapshotID string,
	attempt AttemptRecord,
	workspaceSnapshotID string,
	generation uint64,
	input FindingObservationInput,
) (FindingRecord, error) {
	if err := s.ensureFindingStore(); err != nil {
		return FindingRecord{}, err
	}
	evidence := canonicalFindingEvidence(input.Evidence)
	id := findingID(
		s.identity.LineageID,
		projectSnapshotID,
		attempt.AttemptID,
		attempt.SemanticTaskRef,
		workspaceSnapshotID,
		generation,
		input.Producer,
		input.ProofClass,
		evidence,
		input.ProblemKey,
	)
	if existing, err := s.LoadFinding(id); err == nil {
		return existing, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return FindingRecord{}, err
	}

	binding, err := s.loadOrCreateProblemBinding(input.ProblemKey, id)
	if err != nil {
		return FindingRecord{}, err
	}
	record := FindingRecord{
		SchemaVersion:              controllerSchemaVersion,
		FindingID:                  id,
		RepositoryIdentity:         s.identity.LineageID,
		ProjectSnapshotID:          projectSnapshotID,
		SourceAttemptID:            attempt.AttemptID,
		SourceSemanticTaskRef:      attempt.SemanticTaskRef,
		SourceWorkspaceSnapshotID:  workspaceSnapshotID,
		SourceControllerGeneration: generation,
		Producer:                   input.Producer,
		ProofClass:                 input.ProofClass,
		Evidence:                   evidence,
		ProblemKey:                 input.ProblemKey,
		CanonicalFindingID:         binding.CanonicalFindingID,
		ObservedAt:                 time.Now().UTC(),
	}
	if err := writeJSONAtomic(s.findingPath(id), record); err != nil {
		return FindingRecord{}, err
	}
	return record, nil
}

func (s *Store) ResolveFinding(findingID string, decision FindingDecision) (FindingDispositionResult, error) {
	lock, err := s.acquireMutationLock()
	if err != nil {
		return FindingDispositionResult{}, err
	}
	defer func() { _ = lock.Close() }()

	finding, err := s.LoadFinding(findingID)
	if err != nil {
		return FindingDispositionResult{}, err
	}
	if existing, err := s.LoadFindingDisposition(findingID); err == nil {
		return resultForCommittedFinding(finding, existing), nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return FindingDispositionResult{}, err
	}
	if decision.Kind == FindingDecisionAmbiguous {
		return unresolvedFindingResult(finding, "semantic disposition remains unresolved"), nil
	}
	head, sourceLive, sourceTerminal, err := s.findingResolutionAuthority(finding)
	if err != nil {
		return FindingDispositionResult{}, err
	}
	return s.resolveFindingDecision(finding, head, decision, sourceLive, sourceTerminal)
}

func (s *Store) findingResolutionAuthority(
	finding FindingRecord,
) (RepositoryControllerHead, bool, bool, error) {
	head, err := s.LoadHead()
	if err != nil {
		return RepositoryControllerHead{}, false, false, err
	}
	sourceAttempt, err := s.loadAttempt(finding.SourceAttemptID)
	if err != nil {
		return RepositoryControllerHead{}, false, false, err
	}
	sourceLive := sourceAttempt.AttemptState == AttemptStateLive &&
		head.LiveAttemptID == finding.SourceAttemptID &&
		head.ExecutionTaskRef != nil &&
		head.ExecutionTaskRef.Equal(finding.SourceSemanticTaskRef)
	sourceTerminal := sourceAttempt.AttemptState == AttemptStateAccepted
	if !sourceLive && !sourceTerminal {
		return RepositoryControllerHead{}, false, false,
			fmt.Errorf("finding source attempt is neither current live execution nor accepted terminal history")
	}
	if sourceLive && head.ControllerGeneration != finding.SourceControllerGeneration {
		return RepositoryControllerHead{}, false, false, fmt.Errorf("finding source controller generation is stale")
	}
	return head, sourceLive, sourceTerminal, nil
}

func (s *Store) resolveFindingDecision(
	finding FindingRecord,
	head RepositoryControllerHead,
	decision FindingDecision,
	sourceLive bool,
	sourceTerminal bool,
) (FindingDispositionResult, error) {
	switch decision.Kind {
	case FindingDecisionSameTask:
		return s.resolveSameTaskFinding(finding, head, sourceLive)
	case FindingDecisionIndependentNonBlocking:
		return s.resolveIndependentFinding(finding, head, decision, false, sourceLive, sourceTerminal)
	case FindingDecisionIndependentBlocking:
		if sourceTerminal {
			return unresolvedFindingResult(finding, "accepted terminal work cannot be reopened as blocker interruption"), nil
		}
		return s.resolveIndependentFinding(finding, head, decision, true, sourceLive, false)
	case FindingDecisionDuplicate:
		return s.resolveDuplicateFinding(finding, head)
	case FindingDecisionNonActionable:
		return s.resolveNonActionableFinding(finding, head)
	default:
		return FindingDispositionResult{}, fmt.Errorf("unsupported finding decision %s", decision.Kind)
	}
}

func (s *Store) resolveNonActionableFinding(
	finding FindingRecord,
	head RepositoryControllerHead,
) (FindingDispositionResult, error) {
	disposition, err := s.commitFindingDisposition(finding, head.ControllerGeneration, FindingDisposition{
		Kind: FindingDispositionNonActionable,
	})
	if err != nil {
		return FindingDispositionResult{}, err
	}
	return FindingDispositionResult{Finding: finding, Disposition: &disposition, Intent: FindingIntentNoAction}, nil
}

func unresolvedFindingResult(finding FindingRecord, reason string) FindingDispositionResult {
	return FindingDispositionResult{
		Finding: finding,
		Intent:  FindingIntentAwaitingDisposition,
		Reason:  reason,
	}
}

func (s *Store) resolveSameTaskFinding(
	finding FindingRecord,
	head RepositoryControllerHead,
	sourceLive bool,
) (FindingDispositionResult, error) {
	if !sourceLive {
		return unresolvedFindingResult(finding, "same-task correction requires the exact live source attempt"), nil
	}
	if finding.ProofClass != FindingProofAttemptMutation {
		return unresolvedFindingResult(finding, "same-task proof is insufficient"), nil
	}
	target := finding.SourceSemanticTaskRef
	disposition, err := s.commitFindingDisposition(finding, head.ControllerGeneration, FindingDisposition{
		Kind:          FindingDispositionSameTask,
		TargetTaskRef: &target,
	})
	if err != nil {
		return FindingDispositionResult{}, err
	}
	return FindingDispositionResult{
		Finding: finding, Disposition: &disposition, Intent: FindingIntentSameTaskCorrection,
	}, nil
}

func (s *Store) resolveIndependentFinding(
	finding FindingRecord,
	head RepositoryControllerHead,
	decision FindingDecision,
	blocking bool,
	sourceLive bool,
	sourceTerminal bool,
) (FindingDispositionResult, error) {
	target, err := s.independentFindingTarget(finding, head, decision, blocking, sourceLive, sourceTerminal)
	if err != nil {
		return FindingDispositionResult{}, err
	}
	if blocking {
		return s.resolveBlockingFinding(finding, head, decision, target)
	}
	return s.resolveNonBlockingFinding(finding, head, target)
}

func (s *Store) independentFindingTarget(
	finding FindingRecord,
	head RepositoryControllerHead,
	decision FindingDecision,
	blocking bool,
	sourceLive bool,
	sourceTerminal bool,
) (SemanticTaskRef, error) {
	if decision.TargetTaskRef == nil || decision.TargetTaskRef.Empty() {
		return SemanticTaskRef{}, fmt.Errorf("independent finding requires an exact semantic target task")
	}
	target := *decision.TargetTaskRef
	if target.Equal(finding.SourceSemanticTaskRef) {
		return SemanticTaskRef{}, fmt.Errorf("independent finding target must differ from source execution task")
	}
	if blocking && (!sourceLive || decision.BlockingBoundary == "") {
		return SemanticTaskRef{}, fmt.Errorf("blocking finding requires the live source attempt and an exact blocked boundary")
	}
	projectID := head.ProjectSnapshotID
	if sourceTerminal && projectID == "" {
		projectID = finding.ProjectSnapshotID
	}
	project, err := s.LoadProjectSnapshot(projectID)
	if err != nil {
		return SemanticTaskRef{}, err
	}
	if !projectHasTask(project, target) {
		return SemanticTaskRef{}, fmt.Errorf("independent finding target is not in current project authority")
	}
	return target, nil
}

func (s *Store) resolveNonBlockingFinding(
	finding FindingRecord,
	head RepositoryControllerHead,
	target SemanticTaskRef,
) (FindingDispositionResult, error) {
	binding, err := s.bindFindingTarget(finding, target)
	if err != nil {
		return FindingDispositionResult{}, err
	}
	if binding.TargetTaskRef == nil || !binding.TargetTaskRef.Equal(target) {
		return FindingDispositionResult{}, fmt.Errorf("finding problem is already bound to a different semantic target")
	}
	target = *binding.TargetTaskRef
	disposition, err := s.commitFindingDisposition(finding, head.ControllerGeneration, FindingDisposition{
		Kind:          FindingDispositionIndependentNonBlocking,
		TargetTaskRef: &target,
	})
	if err != nil {
		return FindingDispositionResult{}, err
	}
	return FindingDispositionResult{
		Finding: finding, Disposition: &disposition, Intent: FindingIntentRegisterNonBlocking,
	}, nil
}

func (s *Store) resolveDuplicateFinding(
	finding FindingRecord,
	head RepositoryControllerHead,
) (FindingDispositionResult, error) {
	if finding.CanonicalFindingID == finding.FindingID {
		return unresolvedFindingResult(finding, "finding is already the canonical observation for its problem identity"), nil
	}
	binding, err := s.loadProblemBinding(finding.ProblemKey)
	if err != nil {
		return FindingDispositionResult{}, err
	}
	disposition := FindingDisposition{
		Kind:               FindingDispositionDuplicate,
		CanonicalFindingID: binding.CanonicalFindingID,
	}
	if binding.TargetTaskRef != nil {
		target := *binding.TargetTaskRef
		disposition.TargetTaskRef = &target
	}
	committed, err := s.commitFindingDisposition(finding, head.ControllerGeneration, disposition)
	if err != nil {
		return FindingDispositionResult{}, err
	}
	return FindingDispositionResult{
		Finding: finding, Disposition: &committed, Intent: FindingIntentDuplicate,
	}, nil
}

func (s *Store) commitFindingDisposition(
	finding FindingRecord,
	generation uint64,
	disposition FindingDisposition,
) (FindingDisposition, error) {
	disposition.SchemaVersion = controllerSchemaVersion
	disposition.FindingID = finding.FindingID
	if disposition.CanonicalFindingID == "" {
		disposition.CanonicalFindingID = finding.CanonicalFindingID
	}
	disposition.ControllerGeneration = generation
	disposition.DispositionID = findingDispositionID(disposition)
	disposition.CommittedAt = time.Now().UTC()
	if err := writeJSONAtomic(s.findingDispositionPath(finding.FindingID), disposition); err != nil {
		return FindingDisposition{}, err
	}
	return disposition, nil
}

func (s *Store) LoadFinding(id string) (FindingRecord, error) {
	var finding FindingRecord
	if err := readJSON(s.findingPath(id), &finding); err != nil {
		return FindingRecord{}, err
	}
	if finding.SchemaVersion != controllerSchemaVersion ||
		finding.RepositoryIdentity != s.identity.LineageID ||
		finding.FindingID != id ||
		findingID(
			finding.RepositoryIdentity,
			finding.ProjectSnapshotID,
			finding.SourceAttemptID,
			finding.SourceSemanticTaskRef,
			finding.SourceWorkspaceSnapshotID,
			finding.SourceControllerGeneration,
			finding.Producer,
			finding.ProofClass,
			finding.Evidence,
			finding.ProblemKey,
		) != id {
		return FindingRecord{}, fmt.Errorf("finding %s identity is invalid", id)
	}
	if finding.CanonicalFindingID == "" {
		return FindingRecord{}, fmt.Errorf("finding %s has no canonical finding identity", id)
	}
	return finding, nil
}

func (s *Store) LoadFindingDisposition(findingID string) (FindingDisposition, error) {
	var disposition FindingDisposition
	if err := readJSON(s.findingDispositionPath(findingID), &disposition); err != nil {
		return FindingDisposition{}, err
	}
	if disposition.SchemaVersion != controllerSchemaVersion ||
		disposition.FindingID != findingID ||
		disposition.DispositionID != findingDispositionID(disposition) {
		return FindingDisposition{}, fmt.Errorf("finding disposition %s identity is invalid", findingID)
	}
	return disposition, nil
}

func resultForCommittedFinding(
	finding FindingRecord,
	disposition FindingDisposition,
) FindingDispositionResult {
	intent := FindingIntentNoAction
	switch disposition.Kind {
	case FindingDispositionSameTask:
		intent = FindingIntentSameTaskCorrection
	case FindingDispositionIndependentNonBlocking:
		intent = FindingIntentRegisterNonBlocking
	case FindingDispositionIndependentBlocking:
		if disposition.BlockingBoundary != "" {
			intent = FindingIntentOpenBlockerEpisode
		}
	case FindingDispositionDuplicate:
		intent = FindingIntentDuplicate
	}
	return FindingDispositionResult{Finding: finding, Disposition: &disposition, Intent: intent}
}

func projectHasTask(project ProjectSnapshot, task SemanticTaskRef) bool {
	for _, candidate := range project.Tasks {
		if candidate.Equal(task) {
			return true
		}
	}
	return false
}

func canonicalFindingEvidence(evidence []FindingEvidenceRef) []FindingEvidenceRef {
	result := append([]FindingEvidenceRef(nil), evidence...)
	sort.Slice(result, func(i, j int) bool {
		if result[i].Kind != result[j].Kind {
			return result[i].Kind < result[j].Kind
		}
		return result[i].ID < result[j].ID
	})
	return result
}

func findingID(
	repositoryIdentity string,
	projectSnapshotID string,
	attemptID string,
	task SemanticTaskRef,
	workspaceSnapshotID string,
	generation uint64,
	producer string,
	proof FindingProofClass,
	evidence []FindingEvidenceRef,
	problemKey string,
) string {
	parts := []string{
		"controller-finding-v1",
		repositoryIdentity,
		projectSnapshotID,
		attemptID,
		task.TaskPath,
		task.ContractDigest,
		workspaceSnapshotID,
		fmt.Sprintf("%d", generation),
		producer,
		string(proof),
		problemKey,
	}
	for _, ref := range canonicalFindingEvidence(evidence) {
		parts = append(parts, string(ref.Kind), ref.ID)
	}
	return digestStrings(parts...)
}

func findingDispositionID(disposition FindingDisposition) string {
	parts := []string{
		"controller-finding-disposition-v1",
		disposition.FindingID,
		disposition.CanonicalFindingID,
		string(disposition.Kind),
		fmt.Sprintf("%d", disposition.ControllerGeneration),
		disposition.BlockingBoundary,
		disposition.EpisodeID,
		fmt.Sprintf("%d", disposition.EpisodeRevision),
	}
	if disposition.TargetTaskRef != nil {
		parts = append(parts, disposition.TargetTaskRef.TaskPath, disposition.TargetTaskRef.ContractDigest)
	}
	return digestStrings(parts...)
}

func (s *Store) ensureFindingStore() error {
	for _, name := range []string{"findings", "finding-dispositions", "finding-problems", "episodes"} {
		if err := os.MkdirAll(filepath.Join(s.dir, name), 0o700); err != nil {
			return fmt.Errorf("create controller finding store: %w", err)
		}
	}
	return nil
}

func (s *Store) loadOrCreateProblemBinding(problemKey, findingID string) (findingProblemBinding, error) {
	binding, err := s.loadProblemBinding(problemKey)
	if err == nil {
		return binding, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return findingProblemBinding{}, err
	}
	binding = findingProblemBinding{
		SchemaVersion:      controllerSchemaVersion,
		ProblemKey:         problemKey,
		CanonicalFindingID: findingID,
		UpdatedAt:          time.Now().UTC(),
	}
	if err := writeJSONAtomic(s.findingProblemPath(problemKey), binding); err != nil {
		return findingProblemBinding{}, err
	}
	return binding, nil
}

func (s *Store) bindFindingTarget(finding FindingRecord, target SemanticTaskRef) (findingProblemBinding, error) {
	binding, err := s.loadProblemBinding(finding.ProblemKey)
	if err != nil {
		return findingProblemBinding{}, err
	}
	if binding.TargetTaskRef != nil {
		return binding, nil
	}
	binding.TargetTaskRef = &target
	binding.UpdatedAt = time.Now().UTC()
	if err := writeJSONAtomic(s.findingProblemPath(finding.ProblemKey), binding); err != nil {
		return findingProblemBinding{}, err
	}
	return binding, nil
}

func (s *Store) loadProblemBinding(problemKey string) (findingProblemBinding, error) {
	var binding findingProblemBinding
	if err := readJSON(s.findingProblemPath(problemKey), &binding); err != nil {
		return findingProblemBinding{}, err
	}
	if binding.SchemaVersion != controllerSchemaVersion ||
		binding.ProblemKey != problemKey ||
		binding.CanonicalFindingID == "" {
		return findingProblemBinding{}, fmt.Errorf("finding problem binding is invalid")
	}
	return binding, nil
}

func (s *Store) findingPath(id string) string {
	return filepath.Join(s.dir, "findings", id+".json")
}

func (s *Store) findingDispositionPath(id string) string {
	return filepath.Join(s.dir, "finding-dispositions", id+".json")
}

func (s *Store) findingProblemPath(problemKey string) string {
	return filepath.Join(s.dir, "finding-problems", digestStrings("finding-problem-v1", problemKey)+".json")
}
