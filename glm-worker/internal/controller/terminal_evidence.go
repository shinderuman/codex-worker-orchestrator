package controller

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const taskStatusTerminal = "terminal"

func (s *Store) LoadTerminalTaskRecord(ref EvidenceObjectRef) (TerminalTaskRecord, error) {
	if err := validateTypedEvidenceRef(ref, "terminal-task"); err != nil {
		return TerminalTaskRecord{}, err
	}
	data, err := s.LoadEvidenceObject(ref)
	if err != nil {
		return TerminalTaskRecord{}, err
	}
	var record TerminalTaskRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return record, err
	}
	if record.SchemaVersion != controllerSchemaVersion || record.RepositoryIdentity != s.identity.LineageID || record.TransitionID != ref.LogicalIdentity || record.TaskRef.Empty() || record.AttemptID == "" || record.SourceProject.SnapshotID != projectSnapshotID(record.SourceProject) || record.ResultProject.SnapshotID != projectSnapshotID(record.ResultProject) {
		return record, fmt.Errorf("terminal Task record authority is invalid")
	}
	if err := validateTerminalProjectBindings(record); err != nil {
		return record, err
	}
	return record, nil
}

func (s *Store) verifyTerminalTaskRecord(ref EvidenceObjectRef) error {
	record, err := s.LoadTerminalTaskRecord(ref)
	if err != nil {
		return err
	}
	c, err := s.LoadAcceptedCandidate(record.CandidateRef)
	if err != nil {
		return err
	}
	if !terminalCandidateMatches(record, c) {
		return fmt.Errorf("terminal Task record lacks exact completion candidate proof")
	}
	roots, err := s.VerifyGitObjectArchive(record.GitArchive)
	if err != nil {
		return err
	}
	if len(roots) != 1 || roots[0].OID != record.ResultProject.HeadOID || roots[0].Type != gitCommitObjectType {
		return fmt.Errorf("terminal metadata archive has wrong root")
	}
	return nil
}

func terminalCandidateMatches(record TerminalTaskRecord, c AcceptedCandidate) bool {
	return c.State == candidateObserved && c.ImplementationRef != nil && c.TaskRef.Equal(record.TaskRef) && c.AttemptID == record.AttemptID && c.CommitOID == record.CandidateOID && c.ProjectSnapshotID == record.SourceProject.SnapshotID
}

func (s *Store) validateTerminalMetadataOperation(op ExecutionOperation) error {
	p := op.Terminal
	if !completeTerminalMetadataPayload(op) {
		return fmt.Errorf("terminal metadata operation is incomplete")
	}
	if err := s.verifyTerminalTaskRecord(p.RecordRef); err != nil {
		return err
	}
	record, err := s.LoadTerminalTaskRecord(p.RecordRef)
	if err != nil {
		return err
	}
	candidate, err := s.LoadAcceptedCandidate(record.CandidateRef)
	if err != nil {
		return err
	}
	implementation, err := s.completionImplementation(candidate)
	if err != nil {
		return err
	}
	if p.Policy != candidate.Policy {
		return fmt.Errorf("terminal publication policy differs from candidate")
	}
	if err := validateCompletionSourceBinding(op, record, candidate, implementation); err != nil {
		return err
	}
	if err := validateTerminalMetadataJournal(op, record, implementation.CommitOID); err != nil {
		return err
	}
	for _, file := range p.Files {
		if err := s.validateTerminalMetadataFile(record, file); err != nil {
			return err
		}
	}
	if err := validateCompletionEvidenceShape(op, op.Evidence); err != nil {
		return err
	}
	return s.validateCompletionEpisode(op)
}

func completeTerminalMetadataPayload(op ExecutionOperation) bool {
	return op.Transition.Kind == publicationPublish && op.Terminal != nil && op.Publication == nil && op.SealRef != nil && op.Terminal.RootTaskRef != nil && len(op.Terminal.Files) >= 2
}

func bindingContainsPath(bindings []MetadataTaskBinding, path string) bool {
	for _, binding := range bindings {
		if binding.Source.TaskPath == path && binding.Result.TaskPath == path {
			return true
		}
	}
	return false
}

func (s *Store) terminalMetadataEvidence(op ExecutionOperation) (EvidencePublicationInput, error) {
	c, err := s.LoadAcceptedCandidate(op.Terminal.Record.CandidateRef)
	if err != nil {
		return EvidencePublicationInput{}, err
	}
	head, err := s.LoadHead()
	if err != nil {
		return EvidencePublicationInput{}, err
	}
	_, _, previous, _, err := s.loadPublishedEvidenceAuthority(head)
	if err != nil {
		return EvidencePublicationInput{}, err
	}
	task, err := s.publicationTaskIndex(previous, c)
	if err != nil {
		return EvidencePublicationInput{}, err
	}
	refs, err := s.terminalMetadataLineageRefs(op.Terminal.RecordRef)
	if err != nil {
		return EvidencePublicationInput{}, err
	}
	task.TerminalRecord = &op.Terminal.RecordRef
	task.SemanticStatus = taskStatusTerminal
	task.PublicationLineageRecords = append(task.PublicationLineageRecords, refs...)
	task.ControllerGeneration = op.Transition.CommittedGeneration
	task.TransitionID = op.Transition.TransitionID
	task.CreatedAt = op.Transition.CreatedAt
	final, err := s.storeOperationFinalization(op, c.SealRef, refs)
	if err != nil {
		return EvidencePublicationInput{}, err
	}
	task.Finalizations = append(task.Finalizations, final)
	taskRef, _, err := s.StoreTaskIndexRevision(task)
	if err != nil {
		return EvidencePublicationInput{}, err
	}
	result := EvidencePublicationInput{FinalizationRefs: []EvidenceObjectRef{final}, TaskRevisionRefs: []EvidenceObjectRef{taskRef}}
	return s.terminalMetadataEpisodeEvidence(op, previous, c, taskRef, refs, final, result)

}

func (s *Store) ensureTerminalMetadataFinalized(head RepositoryControllerHead) error {
	if head.PendingTransitionID != "" {
		return fmt.Errorf("integrated Task awaits metadata retirement/finalization")
	}
	if head.AcceptedCandidateRef != nil {
		candidate, err := s.LoadAcceptedCandidate(*head.AcceptedCandidateRef)
		if err != nil {
			return err
		}
		if candidate.EvidenceValid {
			return fmt.Errorf("accepted Task awaits local retirement and publication")
		}
	}
	if head.MetadataLineageRef == nil {
		return nil
	}
	record, err := s.LoadTerminalTaskRecord(*head.MetadataLineageRef)
	if err != nil {
		return err
	}
	_, phase, err := s.LoadTransition(record.TransitionID)
	if err != nil {
		return err
	}
	if phase.Phase != TransitionPhaseFinalized {
		return fmt.Errorf("terminal metadata evidence is not finalized")
	}
	return nil
}

func (s *Store) metadataTaskBindingProven(source, result SemanticTaskRef) bool {
	if source.Equal(result) {
		return true
	}
	if s.amendmentTaskBindingProven(source, result) {
		return true
	}
	head, err := s.LoadHead()
	if err != nil || head.MetadataLineageRef == nil {
		return false
	}
	current := *head.MetadataLineageRef
	wanted := result
	seen := map[string]bool{}
	for {
		if seen[current.Digest] {
			return false
		}
		seen[current.Digest] = true
		record, previous, err := s.metadataTaskBindingStep(current, wanted)
		if err != nil {
			return false
		}
		wanted = previous
		if wanted.Equal(source) {
			return true
		}
		if record.Previous == nil {
			return false
		}
		current = *record.Previous
	}
}

func (s *Store) amendmentTaskBindingProven(source, result SemanticTaskRef) bool {
	head, err := s.LoadHead()
	if err != nil || head.AcceptedCandidateRef == nil {
		return false
	}
	candidate, err := s.LoadAcceptedCandidate(*head.AcceptedCandidateRef)
	if err != nil {
		return false
	}
	seen := map[string]bool{}
	for !seen[candidate.RevisionID] {
		seen[candidate.RevisionID] = true
		if s.candidateAmendmentBinds(candidate, source, result) {
			return true
		}
		if candidate.Previous == nil {
			return false
		}
		previous, err := s.LoadAcceptedCandidate(*candidate.Previous)
		if err != nil {
			return false
		}
		candidate = previous
	}
	return false
}

func (s *Store) candidateAmendmentBinds(candidate AcceptedCandidate, source, result SemanticTaskRef) bool {
	record, phase, err := s.LoadTransition(candidate.TransitionID)
	if err != nil {
		return false
	}
	if record.Kind != publicationRebind || phase.Phase != TransitionPhaseFinalized {
		return false
	}
	if record.SourceRootTaskRef.TaskPath != record.TargetRootTaskRef.TaskPath ||
		record.SourceExecutionTaskRef.TaskPath != record.TargetExecutionTaskRef.TaskPath {
		return false
	}
	execution := record.SourceExecutionTaskRef.Equal(source) && record.TargetExecutionTaskRef.Equal(result)
	root := record.SourceRootTaskRef.Equal(source) && record.TargetRootTaskRef.Equal(result)
	return execution || root
}

func (s *Store) terminalAttemptProven(attempt AttemptRecord) (bool, error) {
	task, err := s.terminalAttemptTaskIndex(attempt)
	if err != nil || task == nil {
		return false, err
	}
	if task.TerminalRecord == nil || task.SemanticStatus != "terminal" {
		return false, nil
	}
	record, err := s.LoadTerminalTaskRecord(*task.TerminalRecord)
	if err != nil {
		return false, err
	}
	if err := s.verifyTerminalTaskRecord(*task.TerminalRecord); err != nil {
		return false, err
	}
	_, phase, err := s.LoadTransition(record.TransitionID)
	if err != nil {
		return false, err
	}
	if record.AttemptID != attempt.AttemptID || !record.TaskRef.Equal(attempt.SemanticTaskRef) || phase.Phase != TransitionPhaseFinalized {
		return false, nil
	}
	return s.terminalTaskPublicationProven(*task, record)
}

func (s *Store) terminalAttemptTaskIndex(attempt AttemptRecord) (*TaskIndexRevision, error) {
	head, err := s.LoadHead()
	if err != nil {
		return nil, err
	}
	if head.EvidenceHeadRef == nil {
		return nil, nil
	}
	evidence, err := s.LoadEvidenceHead(*head.EvidenceHeadRef)
	if err != nil {
		return nil, err
	}
	subject, ok := evidenceSubjectHead(evidence.TaskHeads, taskEvidenceSubjectID(attempt.SemanticTaskRef))
	if !ok {
		return nil, nil
	}
	task, err := s.LoadTaskIndexRevision(subject.RevisionRef)
	if err != nil {
		return nil, err
	}
	return &task, nil
}

func (s *Store) terminalTaskPublicationProven(task TaskIndexRevision, terminal TerminalTaskRecord) (bool, error) {
	seen := map[string]bool{}
	for {
		record, phase, err := s.LoadTransition(task.TransitionID)
		if err != nil {
			return false, err
		}
		if record.Kind == publicationPublish {
			return s.verifyTerminalPublicationOperation(record, phase, terminal)
		}
		previous, err := s.previousTerminalTaskIndex(task, seen)
		if err != nil || previous == nil {
			return false, err
		}
		task = *previous
	}
}

func (s *Store) previousTerminalTaskIndex(task TaskIndexRevision, seen map[string]bool) (*TaskIndexRevision, error) {
	if task.PreviousRevision == nil {
		return nil, nil
	}
	ref := *task.PreviousRevision
	if seen[ref.Digest] {
		return nil, fmt.Errorf("terminal Task index lineage is cyclic")
	}
	seen[ref.Digest] = true
	previous, err := s.LoadTaskIndexRevision(ref)
	if err != nil {
		return nil, err
	}
	if !previous.TaskRef.Equal(task.TaskRef) || previous.TerminalRecord == nil || task.TerminalRecord == nil || !evidenceRefsEqual(*previous.TerminalRecord, *task.TerminalRecord) {
		return nil, fmt.Errorf("terminal Task publication lineage changes Task or terminal record")
	}
	return &previous, nil
}

func (s *Store) verifyTerminalPublicationOperation(record TransitionRecord, phase TransitionState, terminal TerminalTaskRecord) (bool, error) {
	if record.Kind != publicationPublish || phase.Phase != TransitionPhaseFinalized {
		return false, nil
	}
	var op ExecutionOperation
	if err := readJSON(s.executionOperationPath(record.TransitionID), &op); err != nil {
		return false, err
	}
	if !reflect.DeepEqual(record, op.Transition) {
		return false, fmt.Errorf("terminal publication journal differs")
	}
	if _, err := s.validateExecutionOperation(op); err != nil {
		return false, err
	}
	if op.Terminal == nil || !evidenceRefsEqual(op.Terminal.Record.CandidateRef, terminal.CandidateRef) || !reflect.DeepEqual(op.Terminal.Record, terminal) {
		return false, fmt.Errorf("terminal publication candidate differs")
	}
	if _, err := s.completionObservation(op, phase); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) validateTerminalMetadataFile(record TerminalTaskRecord, file TerminalMetadataFile) error {
	if !state.IsParentManagedPath(file.Path) || (file.Path != state.ParentPlanFile && file.Path != record.TaskRef.TaskPath && !bindingContainsPath(record.Bindings, file.Path)) {
		return fmt.Errorf("terminal metadata path is outside exact retirement ownership")
	}
	old, err := readCommittedObject(s.identity.PrimaryRoot, record.SourceProject.HeadOID, file.Path)
	if err != nil || !reflect.DeepEqual(old, file.OldBytes) {
		return fmt.Errorf("terminal metadata old bytes differ from source project")
	}
	if file.NewOID != "" {
		newBytes, err := readCommittedObject(s.identity.PrimaryRoot, record.ResultProject.HeadOID, file.Path)
		if err != nil || !reflect.DeepEqual(newBytes, file.NewBytes) {
			return fmt.Errorf("terminal metadata new bytes differ from result project")
		}
	} else if file.Path != record.TaskRef.TaskPath {
		return fmt.Errorf("terminal metadata deletes unrelated Task")
	}
	return nil
}

func (s *Store) metadataTaskBindingStep(current EvidenceObjectRef, wanted SemanticTaskRef) (TerminalTaskRecord, SemanticTaskRef, error) {
	record, err := s.LoadTerminalTaskRecord(current)
	if err != nil {
		return record, wanted, fmt.Errorf("terminal metadata binding is not finalized")
	}
	_, phase, err := s.LoadTransition(record.TransitionID)
	if err != nil || phase.Phase != TransitionPhaseFinalized {
		return record, wanted, fmt.Errorf("terminal metadata binding is not finalized")
	}
	for _, binding := range record.Bindings {
		if binding.Result.Equal(wanted) {
			wanted = binding.Source
			break
		}
	}
	return record, wanted, nil
}

func validateTerminalMetadataJournal(op ExecutionOperation, record TerminalTaskRecord, implementation string) error {
	p := op.Terminal
	effects := terminalMetadataEffects(*p, implementation)
	effects = append(effects, EffectExpectation{Surface: MutationSurfaceHistory, Resource: p.Policy.Remote + ":" + p.Policy.RemoteRef, ExpectedOld: op.Source.Head.IntegrationTip, ExpectedNew: record.CandidateOID})
	if !reflect.DeepEqual(record, p.Record) || record.TransitionID != op.Transition.TransitionID || record.ControllerGeneration != op.Transition.CommittedGeneration || record.SourceProject.SnapshotID != op.Transition.ProjectSnapshotOld || record.ResultProject.SnapshotID != op.Transition.ProjectSnapshotNew || !reflect.DeepEqual(effects, op.Transition.Effects) {
		return fmt.Errorf("terminal metadata journal authority differs")
	}
	if !projectHasTask(record.SourceProject, record.TaskRef) || projectHasTask(record.ResultProject, record.TaskRef) || !projectHasTask(record.ResultProject, *p.RootTaskRef) {
		return fmt.Errorf("terminal retirement Task/project identity differs")
	}
	return nil
}

func (s *Store) terminalMetadataLineageRefs(root EvidenceObjectRef) ([]EvidenceObjectRef, error) {
	var refs []EvidenceObjectRef
	current := root
	seen := map[string]bool{}
	for {
		if seen[current.Digest] {
			return nil, fmt.Errorf("terminal metadata lineage cycle")
		}
		seen[current.Digest] = true
		record, err := s.LoadTerminalTaskRecord(current)
		if err != nil {
			return nil, err
		}
		candidate, err := s.LoadAcceptedCandidate(record.CandidateRef)
		if err != nil {
			return nil, err
		}
		candidateRefs, err := s.candidatePublicationRefs(candidate, record.CandidateRef, nil)
		if err != nil {
			return nil, err
		}
		seal, err := s.LoadAttemptSeal(candidate.SealRef)
		if err != nil {
			return nil, err
		}
		refs = append(refs, current, record.GitArchive, candidate.SealRef, seal.GitObjectArchive)
		refs = append(refs, candidateRefs...)
		refs = append(refs, seal.EvidenceRefs...)
		if record.Previous == nil {
			return canonicalEvidenceRefs(refs), nil
		}
		current = *record.Previous
	}
}

func (s *Store) validateTerminalRecoverySource(op ExecutionOperation, head RepositoryControllerHead) error {
	p := op.Terminal
	if head.ProjectSnapshotID != op.Transition.ProjectSnapshotOld || head.AcceptedCandidateRef == nil || head.LiveLeaseID != "" {
		return fmt.Errorf("terminal recovery source authority changed")
	}
	candidate, err := s.LoadAcceptedCandidate(p.Record.CandidateRef)
	if err != nil {
		return err
	}
	if candidate.ImplementationRef == nil || !evidenceRefsEqual(*head.AcceptedCandidateRef, *candidate.ImplementationRef) || head.IntegrationTip != candidate.BaseOID {
		return fmt.Errorf("terminal recovery source Task/candidate changed")
	}
	return nil
}

func (s *Store) terminalMetadataEpisodeEvidence(op ExecutionOperation, previous EvidenceHead, c AcceptedCandidate, taskRef EvidenceObjectRef, refs []EvidenceObjectRef, final EvidenceObjectRef, result EvidencePublicationInput) (EvidencePublicationInput, error) {
	if op.Terminal.Episode != nil {
		pub := op
		pub.Publication = &PublicationOperation{NewTip: op.Terminal.Record.ResultProject.HeadOID, Episode: op.Terminal.Episode}
		episode, err := s.publicationEpisodeEvidence(pub, previous, c, taskRef, refs, final)
		if err != nil {
			return result, err
		}
		episode.RootTaskRef = *op.Terminal.RootTaskRef
		episode.State = string(op.Terminal.Episode.State)
		ref, _, err := s.StoreEpisodeIndexRevision(episode)
		if err != nil {
			return result, err
		}
		result.EpisodeRevisionRefs = []EvidenceObjectRef{ref}
	}
	return result, nil
}

func validateTerminalProjectBindings(record TerminalTaskRecord) error {
	for _, project := range []ProjectSnapshot{record.SourceProject, record.ResultProject} {
		if project.SchemaVersion != controllerSchemaVersion || project.RepositoryIdentity != record.RepositoryIdentity {
			return fmt.Errorf("terminal metadata project schema/identity differs")
		}
	}
	seen := map[string]bool{}
	for _, binding := range record.Bindings {
		if binding.Source.TaskPath != binding.Result.TaskPath || binding.Source.Equal(binding.Result) || seen[binding.Source.TaskPath] {
			return fmt.Errorf("terminal metadata binding is ambiguous")
		}
		if !projectHasTask(record.SourceProject, binding.Source) || !projectHasTask(record.ResultProject, binding.Result) {
			return fmt.Errorf("terminal metadata binding is outside source/result project")
		}
		seen[binding.Source.TaskPath] = true
	}
	return nil
}

func validateCompletionSourceBinding(op ExecutionOperation, record TerminalTaskRecord, candidate, implementation AcceptedCandidate) error {
	if op.Source.Head.ControllerGeneration != op.Transition.SourceGeneration || op.Source.Head.ProjectSnapshotID != record.SourceProject.SnapshotID || op.Source.Head.AcceptedCandidateRef == nil || candidate.ImplementationRef == nil || !evidenceRefsEqual(*op.Source.Head.AcceptedCandidateRef, *candidate.ImplementationRef) || implementation.BaseOID != op.Source.Head.IntegrationTip || op.Source.Head.LiveLeaseID != "" {
		return fmt.Errorf("completion source authority differs from the sealed implementation")
	}
	return nil
}

func (s *Store) validateCompletionEpisode(op ExecutionOperation) error {
	expected := *op.Terminal
	expected.Episode = nil
	record := op.Transition
	if err := s.planTerminalMetadataEpisode(op.Source.Head, &expected, &record, op.Terminal.Record.TaskRef); err != nil {
		return err
	}
	if expected.Episode != nil && op.Terminal.Episode != nil {
		expected.Episode.CreatedAt = op.Terminal.Episode.CreatedAt
		expected.Episode.RevisionID = blockerEpisodeRevisionID(*expected.Episode)
	}
	if !reflect.DeepEqual(expected.Episode, op.Terminal.Episode) || record.TargetEpisodeRevision != op.Transition.TargetEpisodeRevision {
		return fmt.Errorf("completion episode target differs from the planned progression")
	}
	return nil
}
