package controller

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

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
	if c.State != candidateObserved || !c.TaskRef.Equal(record.TaskRef) || c.AttemptID != record.AttemptID || c.CommitOID != record.CandidateOID || c.ProjectSnapshotID != record.SourceProject.SnapshotID {
		return fmt.Errorf("terminal Task record lacks exact integrated candidate proof")
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

func (s *Store) validateTerminalMetadataOperation(op ExecutionOperation) error {
	p := op.Terminal
	if p == nil || op.Publication != nil || op.SealRef == nil || p.RootTaskRef == nil || len(p.Files) < 2 {
		return fmt.Errorf("terminal metadata operation is incomplete")
	}
	if err := s.verifyTerminalTaskRecord(p.RecordRef); err != nil {
		return err
	}
	record, err := s.LoadTerminalTaskRecord(p.RecordRef)
	if err != nil {
		return err
	}
	if err := validateTerminalMetadataJournal(op, record); err != nil {
		return err
	}
	for _, file := range p.Files {
		if err := s.validateTerminalMetadataFile(record, file); err != nil {
			return err
		}
	}
	return nil
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
	task.SemanticStatus = "terminal"
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
	if head.PendingTerminalTaskRef != nil || head.PendingTransitionID != "" {
		return fmt.Errorf("integrated Task awaits metadata retirement/finalization")
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

func (s *Store) terminalAttemptProven(attempt AttemptRecord) (bool, error) {
	head, err := s.LoadHead()
	if err != nil {
		return false, err
	}
	if head.EvidenceHeadRef == nil {
		return false, nil
	}
	evidence, err := s.LoadEvidenceHead(*head.EvidenceHeadRef)
	if err != nil {
		return false, err
	}
	subject, ok := evidenceSubjectHead(evidence.TaskHeads, taskEvidenceSubjectID(attempt.SemanticTaskRef))
	if !ok {
		return false, nil
	}
	task, err := s.LoadTaskIndexRevision(subject.RevisionRef)
	if err != nil {
		return false, err
	}
	if task.TerminalRecord == nil {
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
	return record.AttemptID == attempt.AttemptID && record.TaskRef.Equal(attempt.SemanticTaskRef) && phase.Phase == TransitionPhaseFinalized, nil
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

func validateTerminalMetadataJournal(op ExecutionOperation, record TerminalTaskRecord) error {
	p := op.Terminal
	if !reflect.DeepEqual(record, p.Record) || record.TransitionID != op.Transition.TransitionID || record.ControllerGeneration != op.Transition.CommittedGeneration || record.SourceProject.SnapshotID != op.Transition.ProjectSnapshotOld || record.ResultProject.SnapshotID != op.Transition.ProjectSnapshotNew || !reflect.DeepEqual(terminalMetadataEffects(*p), op.Transition.Effects) {
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

func validateTerminalRecoverySource(op ExecutionOperation, head RepositoryControllerHead) error {
	p := op.Terminal
	if head.ProjectSnapshotID != op.Transition.ProjectSnapshotOld || head.IntegrationTip != p.Record.SourceProject.HeadOID || head.AcceptedCandidateRef == nil || head.PendingTerminalTaskRef == nil || head.LiveLeaseID != "" {
		return fmt.Errorf("terminal recovery source authority changed")
	}
	if !evidenceRefsEqual(*head.AcceptedCandidateRef, p.Record.CandidateRef) || !head.PendingTerminalTaskRef.Equal(p.Record.TaskRef) {
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
