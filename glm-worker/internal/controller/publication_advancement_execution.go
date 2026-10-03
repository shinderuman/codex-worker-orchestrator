package controller

import "fmt"

func (s *Store) applyAdvancementSuspension(op ExecutionOperation) error {
	if op.Suspension == nil {
		return s.preflightSuspendedRebind(op.Publication.NewTip)
	}
	if op.SealRef == nil {
		return fmt.Errorf("mutable advancement seal is missing")
	}
	if _, err := RebindSuspensionTrees(s.identity.PrimaryRoot, *op.Suspension, op.Publication.NewTip); err != nil {
		return err
	}
	if err := s.retainSuspension(op.Source.Workspace.Root, *op.Suspension); err != nil {
		return err
	}
	trees, err := CaptureExecutionTrees(op.Source.Workspace.Root, op.Suspension.ExecutionBaseOID)
	if err != nil {
		return err
	}
	if trees != op.Suspension.Current {
		return fmt.Errorf("mutable source changed after advancement prepare")
	}
	return nil
}

func (s *Store) commitAdvancementExecution(next *RepositoryControllerHead, op ExecutionOperation) error {
	if op.Publication.Episode != nil {
		if err := s.writeEpisodeRevision(*op.Publication.Episode); err != nil {
			return err
		}
		next.ActiveEpisodeRevision = op.Publication.Episode.Revision
	}
	if op.Suspension == nil {
		return nil
	}
	attempt := op.Source.Attempt
	attempt.AttemptState = AttemptStateSuspendedForAdvancement
	attempt.AttemptSealID = op.SealRef.LogicalIdentity
	if err := s.writeAttempt(attempt); err != nil {
		return err
	}
	next.LiveAttemptID = ""
	next.LiveLeaseID = ""
	return nil
}

func (s *Store) appendAdvancementLineage(op ExecutionOperation, input EvidencePublicationInput) (EvidencePublicationInput, error) {
	if len(input.TaskRevisionRefs) != 1 || op.Publication.AdvancementRef == nil {
		return input, fmt.Errorf("mutable advancement task evidence is incomplete")
	}
	task, err := s.LoadTaskIndexRevision(input.TaskRevisionRefs[0])
	if err != nil {
		return input, err
	}
	lineage, err := s.putEvidenceJSON("base-advancement", op.Transition.TransitionID, BaseAdvancementRecord{RepositoryIdentity: s.identity.LineageID, OldTip: op.Publication.OldTip, NewTip: op.Publication.NewTip, TransitionID: op.Transition.TransitionID, ControllerGeneration: op.Transition.CommittedGeneration, ProjectSnapshotID: op.Transition.ProjectSnapshotNew, GitArchive: *op.Publication.AdvancementRef})
	if err != nil {
		return input, err
	}
	if op.Suspension == nil {
		task.PreviousRevision = &input.TaskRevisionRefs[0]
	}
	task.ControllerGeneration = op.Transition.CommittedGeneration
	task.TransitionID = op.Transition.TransitionID
	task.CreatedAt = op.Transition.CreatedAt
	task.PublicationLineageRecords = append(task.PublicationLineageRecords, lineage, *op.Publication.AdvancementRef)
	ref, _, err := s.StoreTaskIndexRevision(task)
	if err != nil {
		return input, err
	}
	input.TaskRevisionRefs = []EvidenceObjectRef{ref}
	if len(input.EpisodeRevisionRefs) != 0 {
		episode, err := s.LoadEpisodeIndexRevision(input.EpisodeRevisionRefs[0])
		if err != nil {
			return input, err
		}
		episode.TaskIndexHeads = replaceExecutionTaskHead(episode.TaskIndexHeads, EvidenceSubjectHead{SubjectID: taskEvidenceSubjectID(task.TaskRef), RevisionRef: ref})
		episode.IntegrationHistory = append(episode.IntegrationHistory, lineage, *op.Publication.AdvancementRef)
		episode.IntegrationTip = op.Publication.NewTip
		episodeRef, _, err := s.StoreEpisodeIndexRevision(episode)
		if err != nil {
			return input, err
		}
		input.EpisodeRevisionRefs = []EvidenceObjectRef{episodeRef}
	}
	return input, nil
}

func (s *Store) ordinarySuspendedAdvancementEvidence(op ExecutionOperation, head RepositoryControllerHead, previous EvidenceHead) (EvidencePublicationInput, error) {
	if head.ExecutionTaskRef == nil {
		return EvidencePublicationInput{}, fmt.Errorf("suspended execution Task authority is missing")
	}
	task, ok := evidenceSubjectHead(previous.TaskHeads, taskEvidenceSubjectID(*head.ExecutionTaskRef))
	if !ok {
		return EvidencePublicationInput{}, fmt.Errorf("suspended execution evidence is missing")
	}
	return s.appendAdvancementLineage(op, EvidencePublicationInput{TaskRevisionRefs: []EvidenceObjectRef{task.RevisionRef}})
}
