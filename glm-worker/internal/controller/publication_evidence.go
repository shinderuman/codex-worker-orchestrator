package controller

import "fmt"

func (s *Store) publicationEvidence(op ExecutionOperation, c AcceptedCandidate, lineage EvidenceObjectRef, observation *EvidenceObjectRef) (EvidencePublicationInput, error) {
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
	task.AttemptSeals = canonicalEvidenceRefs(append(task.AttemptSeals, c.SealRef))
	refs, err := s.candidatePublicationRefs(c, lineage, observation)
	if err != nil {
		return EvidencePublicationInput{}, err
	}
	task.PublicationLineageRecords = append(task.PublicationLineageRecords, refs...)
	task.SemanticStatus = "awaiting-publication"
	if c.State == candidateObserved {
		task.SemanticStatus = "published"
	}
	task.ControllerGeneration = op.Transition.CommittedGeneration
	task.TransitionID = op.Transition.TransitionID
	task.CreatedAt = op.Transition.CreatedAt
	final, _, err := s.StoreAttemptFinalization(AttemptFinalizationRecord{SchemaVersion: evidenceSchemaVersion, AttemptSealRef: c.SealRef, ControllerGeneration: op.Transition.CommittedGeneration, TransitionID: op.Transition.TransitionID, ProjectSnapshotID: op.Transition.ProjectSnapshotNew, Kind: op.Transition.Kind, EvidenceRefs: refs, CreatedAt: op.Transition.CreatedAt})
	if err != nil {
		return EvidencePublicationInput{}, err
	}
	task.Finalizations = append(task.Finalizations, final)
	taskRef, _, err := s.StoreTaskIndexRevision(task)
	if err != nil {
		return EvidencePublicationInput{}, err
	}
	result := EvidencePublicationInput{FinalizationRefs: []EvidenceObjectRef{final}, TaskRevisionRefs: []EvidenceObjectRef{taskRef}}
	if op.Transition.Kind == publicationAccept {
		result.AttemptSealRefs = []EvidenceObjectRef{c.SealRef}
	}
	if c.EpisodeID != "" {
		episode, err := s.publicationEpisodeEvidence(op, previous, c, taskRef, refs, final)
		if err != nil {
			return EvidencePublicationInput{}, err
		}
		ref, _, err := s.StoreEpisodeIndexRevision(episode)
		if err != nil {
			return EvidencePublicationInput{}, err
		}
		result.EpisodeRevisionRefs = []EvidenceObjectRef{ref}
	}
	return result, nil
}

func (s *Store) publicationEpisodeEvidence(op ExecutionOperation, previous EvidenceHead, c AcceptedCandidate, taskRef EvidenceObjectRef, lineage []EvidenceObjectRef, final EvidenceObjectRef) (EpisodeIndexRevision, error) {
	old, ok := evidenceSubjectHead(previous.EpisodeHeads, c.EpisodeID)
	if !ok {
		return EpisodeIndexRevision{}, fmt.Errorf("publication episode evidence authority is missing")
	}
	episode, err := s.LoadEpisodeIndexRevision(old.RevisionRef)
	if err != nil {
		return EpisodeIndexRevision{}, err
	}
	episode.PreviousRevision = evidenceRefPointer(old.RevisionRef)
	episode.AttemptSeals = canonicalEvidenceRefs(append(episode.AttemptSeals, c.SealRef))
	episode.Finalizations = append(episode.Finalizations, final)
	episode.IntegrationHistory = append(episode.IntegrationHistory, lineage...)
	episode.TaskIndexHeads = replaceExecutionTaskHead(episode.TaskIndexHeads, EvidenceSubjectHead{SubjectID: taskEvidenceSubjectID(c.TaskRef), RevisionRef: taskRef})
	episode.IntegrationTip = op.Publication.NewTip
	if op.Publication.Episode != nil {
		episode.EpisodeRevision = op.Publication.Episode.Revision
		episode.DependencyGraphSnapshotID = op.Publication.Episode.RevisionID
		episode.AdmittedClosureTaskRefs = op.Publication.Episode.AdmittedClosure
	}
	episode.CurrentExecutionTaskRef = nil
	episode.CurrentAttemptID = ""
	episode.CurrentLeaseID = ""
	episode.ControllerGeneration = op.Transition.CommittedGeneration
	episode.TransitionID = op.Transition.TransitionID
	episode.CreatedAt = op.Transition.CreatedAt
	return episode, nil
}

func (s *Store) publicationTaskIndex(previous EvidenceHead, c AcceptedCandidate) (TaskIndexRevision, error) {
	var err error
	task := TaskIndexRevision{SchemaVersion: evidenceSchemaVersion, TaskRef: c.TaskRef}
	if old, ok := evidenceSubjectHead(previous.TaskHeads, taskEvidenceSubjectID(c.TaskRef)); ok {
		task, err = s.LoadTaskIndexRevision(old.RevisionRef)
		if err != nil {
			return task, err
		}
		task.PreviousRevision = evidenceRefPointer(old.RevisionRef)
	}
	return task, nil
}
