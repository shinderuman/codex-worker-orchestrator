package controller

import "fmt"

func (s *Store) buildSuspensionEvidence(op ExecutionOperation) (EvidencePublicationInput, EvidenceObjectRef, error) {
	snapshot := op.Suspension
	archive, roots, err := s.CaptureGitObjectArchive(op.Source.Workspace.Root, snapshot.AttemptID+":suspension-git", []string{snapshot.ExecutionBaseOID, snapshot.Baseline.IndexTree, snapshot.Baseline.WorktreeTree, snapshot.Current.IndexTree, snapshot.Current.WorktreeTree})
	if err != nil {
		return EvidencePublicationInput{}, EvidenceObjectRef{}, err
	}
	seal := AttemptSeal{
		SchemaVersion: evidenceSchemaVersion, RepositoryIdentity: s.identity.LineageID,
		SemanticTaskRef: snapshot.SemanticTaskRef, RootTaskRef: snapshot.RootTaskRef, AttemptID: snapshot.AttemptID,
		PredecessorAttemptID: op.Source.Attempt.PredecessorAttemptID, EpisodeID: snapshot.EpisodeID, EpisodeRevision: snapshot.EpisodeRevision,
		ControllerGeneration: op.Transition.CommittedGeneration, SealingTransitionID: op.Transition.TransitionID,
		RevokedLeaseID: op.Source.Lease.LeaseID, WorkspaceID: snapshot.WorkspaceID, ExecutionPurpose: op.Source.Lease.Purpose,
		SourceProjectSnapshotID: snapshot.SourceProjectSnapshotID, StartedAt: op.Source.Attempt.CreatedAt, SealedAt: op.Transition.CreatedAt,
		Disposition: string(AttemptStateSuspendedForBlocker), ExecutionBaseOID: snapshot.ExecutionBaseOID,
		BaselineIndexTree: snapshot.Baseline.IndexTree, BaselineWorktreeTree: snapshot.Baseline.WorktreeTree,
		CurrentIndexTree: snapshot.Current.IndexTree, CurrentWorktreeTree: snapshot.Current.WorktreeTree,
		ParentAuthorityDigest: snapshot.Current.ParentAuthorityDigest, OperationalSnapshotID: snapshot.SnapshotID,
		GitObjectArchive: archive, GitObjectArchiveRoots: roots,
	}
	if op.Transition.Kind == publicationAdopt {
		seal.Disposition = string(AttemptStateSuspendedForAdvancement)
	}
	if op.Episode != nil {
		seal.EpisodeID = op.Episode.EpisodeID
		seal.EpisodeRevision = op.Episode.Revision
	}
	if err := s.captureAttemptSemanticEvidence(op, &seal); err != nil {
		return EvidencePublicationInput{}, EvidenceObjectRef{}, err
	}
	runtimeCaptured, err := s.captureAttemptRuntimeEvidence(op.Source, &seal)
	if err != nil {
		return EvidencePublicationInput{}, EvidenceObjectRef{}, err
	}
	applyAttemptSealRuntimeCoverage(&seal, runtimeCaptured)
	sealRef, _, err := s.StoreAttemptSeal(seal)
	if err != nil {
		return EvidencePublicationInput{}, EvidenceObjectRef{}, err
	}
	input, err := s.appendExecutionSealIndexes(op, sealRef)
	return input, sealRef, err
}

func (s *Store) appendExecutionSealIndexes(op ExecutionOperation, sealRef EvidenceObjectRef) (EvidencePublicationInput, error) {
	head, err := s.LoadHead()
	if err != nil {
		return EvidencePublicationInput{}, err
	}
	_, _, previous, _, err := s.loadPublishedEvidenceAuthority(head)
	if err != nil {
		return EvidencePublicationInput{}, err
	}
	task := TaskIndexRevision{SchemaVersion: evidenceSchemaVersion, TaskRef: op.Source.Attempt.SemanticTaskRef}
	if old, ok := evidenceSubjectHead(previous.TaskHeads, taskEvidenceSubjectID(task.TaskRef)); ok {
		task, err = s.LoadTaskIndexRevision(old.RevisionRef)
		if err != nil {
			return EvidencePublicationInput{}, err
		}
		task.PreviousRevision = evidenceRefPointer(old.RevisionRef)
	}
	task.AttemptSeals = append(task.AttemptSeals, sealRef)
	task.SemanticStatus = "blocked"
	if op.Transition.Kind == publicationAdopt {
		task.SemanticStatus = "suspended"
	}
	task.ControllerGeneration = op.Transition.CommittedGeneration
	task.TransitionID = op.Transition.TransitionID
	task.CreatedAt = op.Transition.CreatedAt
	taskRef, _, err := s.StoreTaskIndexRevision(task)
	if err != nil {
		return EvidencePublicationInput{}, err
	}
	if op.Episode == nil {
		return EvidencePublicationInput{AttemptSealRefs: []EvidenceObjectRef{sealRef}, TaskRevisionRefs: []EvidenceObjectRef{taskRef}}, nil
	}
	episode, err := s.suspensionEpisodeIndex(op, previous, sealRef, taskRef)
	if err != nil {
		return EvidencePublicationInput{}, err
	}
	episodeRef, _, err := s.StoreEpisodeIndexRevision(episode)
	if err != nil {
		return EvidencePublicationInput{}, err
	}
	return EvidencePublicationInput{AttemptSealRefs: []EvidenceObjectRef{sealRef}, TaskRevisionRefs: []EvidenceObjectRef{taskRef}, EpisodeRevisionRefs: []EvidenceObjectRef{episodeRef}}, nil
}

func (s *Store) suspensionEpisodeIndex(op ExecutionOperation, previous EvidenceHead, sealRef, taskRef EvidenceObjectRef) (EpisodeIndexRevision, error) {
	episode := EpisodeIndexRevision{SchemaVersion: evidenceSchemaVersion, EpisodeID: op.Episode.EpisodeID}
	if old, ok := evidenceSubjectHead(previous.EpisodeHeads, episode.EpisodeID); ok {
		var err error
		episode, err = s.LoadEpisodeIndexRevision(old.RevisionRef)
		if err != nil {
			return EpisodeIndexRevision{}, err
		}
		episode.PreviousRevision = evidenceRefPointer(old.RevisionRef)
	}
	episode.RootTaskRef = op.Episode.RootTaskRef
	episode.EpisodeRevision = op.Episode.Revision
	episode.DependencyGraphSnapshotID = op.Episode.RevisionID
	episode.AdmittedClosureTaskRefs = append([]SemanticTaskRef(nil), op.Episode.AdmittedClosure...)
	episode.AttemptSeals = append(episode.AttemptSeals, sealRef)
	episode.TaskIndexHeads = replaceExecutionTaskHead(episode.TaskIndexHeads, EvidenceSubjectHead{SubjectID: taskEvidenceSubjectID(op.Source.Attempt.SemanticTaskRef), RevisionRef: taskRef})
	episode.State = "open"
	episode.CurrentExecutionTaskRef = nil
	episode.CurrentAttemptID = ""
	episode.CurrentLeaseID = ""
	episode.IntegrationTip = op.Source.Head.IntegrationTip
	if episode.IntegrationTip == "" {
		episode.IntegrationTip = op.Source.Attempt.ExecutionBaseOID
	}
	episode.ControllerGeneration = op.Transition.CommittedGeneration
	episode.TransitionID = op.Transition.TransitionID
	episode.CreatedAt = op.Transition.CreatedAt
	return episode, nil
}

func replaceExecutionTaskHead(heads []EvidenceSubjectHead, next EvidenceSubjectHead) []EvidenceSubjectHead {
	result := append([]EvidenceSubjectHead(nil), heads...)
	for i, head := range result {
		if head.SubjectID == next.SubjectID {
			result[i] = next
			return result
		}
	}
	return append(result, next)
}

func (s *Store) suspensionSealRef(snapshot SuspensionSnapshot) (EvidenceObjectRef, error) {
	head, err := s.LoadHead()
	if err != nil {
		return EvidenceObjectRef{}, err
	}
	if head.EvidenceHeadRef == nil {
		return EvidenceObjectRef{}, fmt.Errorf("suspension seal evidence is unavailable")
	}
	evidence, err := s.LoadEvidenceHead(*head.EvidenceHeadRef)
	if err != nil {
		return EvidenceObjectRef{}, err
	}
	task, ok := evidenceSubjectHead(evidence.TaskHeads, taskEvidenceSubjectID(snapshot.SemanticTaskRef))
	if !ok {
		return EvidenceObjectRef{}, fmt.Errorf("suspension task evidence is unavailable")
	}
	revision, err := s.LoadTaskIndexRevision(task.RevisionRef)
	if err != nil {
		return EvidenceObjectRef{}, err
	}
	for _, ref := range revision.AttemptSeals {
		seal, err := s.LoadAttemptSeal(ref)
		if err != nil {
			return EvidenceObjectRef{}, err
		}
		if seal.OperationalSnapshotID == snapshot.SnapshotID && seal.AttemptID == snapshot.AttemptID {
			return ref, nil
		}
	}
	return EvidenceObjectRef{}, fmt.Errorf("suspension has no published attempt seal")
}
