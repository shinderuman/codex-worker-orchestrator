package controller

import "fmt"

func (s *Store) commitAuthorityTransitionWithEvidenceLocked(
	record TransitionRecord,
	actual map[string]string,
	input EvidencePublicationInput,
	mutate func(*RepositoryControllerHead) error,
) (RepositoryControllerHead, EvidencePublicationResult, error) {
	current, err := s.LoadHead()
	if err != nil {
		return RepositoryControllerHead{}, EvidencePublicationResult{}, err
	}
	if current.PendingTransitionID != record.TransitionID || current.ControllerGeneration != record.PreparedGeneration {
		return RepositoryControllerHead{}, EvidencePublicationResult{}, fmt.Errorf("transition %s no longer owns evidence publication CAS", record.TransitionID)
	}
	publication, err := s.prepareEvidencePublication(current, record, input)
	if err != nil {
		return RepositoryControllerHead{}, EvidencePublicationResult{}, err
	}
	committed, err := s.commitAuthorityTransitionLocked(record, actual, false, func(next *RepositoryControllerHead) error {
		if mutate != nil {
			if err := mutate(next); err != nil {
				return err
			}
		}
		headRef := publication.EvidenceHeadRef
		ledgerRef := publication.LedgerRecordRef
		next.EvidenceHeadRef = &headRef
		next.EvidenceLedgerHeadRef = &ledgerRef
		next.EvidenceLedgerSequence = publication.LedgerRecord.Sequence
		return nil
	})
	if err != nil {
		return RepositoryControllerHead{}, EvidencePublicationResult{}, err
	}
	return committed, publication, nil
}

func (s *Store) prepareEvidencePublication(
	controllerHead RepositoryControllerHead,
	record TransitionRecord,
	input EvidencePublicationInput,
) (EvidencePublicationResult, error) {
	if record.TransitionID == "" || record.CommittedGeneration == 0 || record.ProjectSnapshotNew == "" {
		return EvidencePublicationResult{}, fmt.Errorf("evidence publication transition authority is incomplete")
	}
	if controllerHead.RepositoryIdentity != s.identity.LineageID || controllerHead.PendingTransitionID != record.TransitionID || controllerHead.ControllerGeneration != record.PreparedGeneration {
		return EvidencePublicationResult{}, fmt.Errorf("evidence publication controller authority is stale")
	}
	if len(input.TaskRevisionRefs) == 0 && len(input.EpisodeRevisionRefs) == 0 && len(input.AttemptSealRefs) == 0 && len(input.FinalizationRefs) == 0 {
		return EvidencePublicationResult{}, fmt.Errorf("evidence publication has no graph changes")
	}

	previousHeadRef, previousLedgerRef, previousHead, previousLedger, err := s.loadPublishedEvidenceAuthority(controllerHead)
	if err != nil {
		return EvidencePublicationResult{}, err
	}
	taskHeads, err := s.nextTaskEvidenceHeads(previousHead.TaskHeads, record, input.TaskRevisionRefs)
	if err != nil {
		return EvidencePublicationResult{}, err
	}
	episodeHeads, err := s.nextEpisodeEvidenceHeads(previousHead.EpisodeHeads, record, input.EpisodeRevisionRefs)
	if err != nil {
		return EvidencePublicationResult{}, err
	}
	if err := s.validatePublicationAttemptSeals(record, input.AttemptSealRefs); err != nil {
		return EvidencePublicationResult{}, err
	}
	if err := s.validatePublicationFinalizations(record, input.FinalizationRefs); err != nil {
		return EvidencePublicationResult{}, err
	}

	evidenceHead := EvidenceHead{
		SchemaVersion:        evidenceSchemaVersion,
		RepositoryIdentity:   s.identity.LineageID,
		PreviousHead:         previousHeadRef,
		TaskHeads:            taskHeads,
		EpisodeHeads:         episodeHeads,
		ControllerGeneration: record.CommittedGeneration,
		ProjectSnapshotID:    record.ProjectSnapshotNew,
	}
	evidenceHeadRef, storedHead, err := s.StoreEvidenceHead(evidenceHead)
	if err != nil {
		return EvidencePublicationResult{}, err
	}
	ledger := EvidenceLedgerRecord{
		SchemaVersion:        evidenceSchemaVersion,
		Sequence:             controllerHead.EvidenceLedgerSequence + 1,
		PreviousRecord:       previousLedgerRef,
		RepositoryIdentity:   s.identity.LineageID,
		ControllerGeneration: record.CommittedGeneration,
		TransitionID:         record.TransitionID,
		ProjectSnapshotID:    record.ProjectSnapshotNew,
		EvidenceHeadRef:      evidenceHeadRef,
	}
	ledgerRef, storedLedger, err := s.StoreEvidenceLedgerRecord(ledger)
	if err != nil {
		return EvidencePublicationResult{}, err
	}
	if previousLedgerRef != nil && storedLedger.Sequence != previousLedger.Sequence+1 {
		return EvidencePublicationResult{}, &EvidenceIntegrityError{Digest: ledgerRef.Digest, Reason: "evidence ledger sequence did not advance exactly once"}
	}
	return EvidencePublicationResult{
		EvidenceHeadRef: evidenceHeadRef,
		LedgerRecordRef: ledgerRef,
		EvidenceHead:    storedHead,
		LedgerRecord:    storedLedger,
	}, nil
}

func (s *Store) loadPublishedEvidenceAuthority(
	head RepositoryControllerHead,
) (*EvidenceObjectRef, *EvidenceObjectRef, EvidenceHead, EvidenceLedgerRecord, error) {
	if head.EvidenceHeadRef == nil && head.EvidenceLedgerHeadRef == nil && head.EvidenceLedgerSequence == 0 {
		return nil, nil, EvidenceHead{}, EvidenceLedgerRecord{}, nil
	}
	if head.EvidenceHeadRef == nil || head.EvidenceLedgerHeadRef == nil || head.EvidenceLedgerSequence == 0 {
		return nil, nil, EvidenceHead{}, EvidenceLedgerRecord{}, fmt.Errorf("repository controller evidence authority is incomplete")
	}
	evidenceHead, err := s.LoadEvidenceHead(*head.EvidenceHeadRef)
	if err != nil {
		return nil, nil, EvidenceHead{}, EvidenceLedgerRecord{}, err
	}
	ledger, err := s.LoadEvidenceLedgerRecord(*head.EvidenceLedgerHeadRef)
	if err != nil {
		return nil, nil, EvidenceHead{}, EvidenceLedgerRecord{}, err
	}
	if ledger.Sequence != head.EvidenceLedgerSequence || !evidenceRefsEqual(ledger.EvidenceHeadRef, *head.EvidenceHeadRef) {
		return nil, nil, EvidenceHead{}, EvidenceLedgerRecord{}, &EvidenceIntegrityError{Digest: head.EvidenceLedgerHeadRef.Digest, Reason: "controller evidence pointers disagree with ledger authority"}
	}
	headRef := *head.EvidenceHeadRef
	ledgerRef := *head.EvidenceLedgerHeadRef
	return &headRef, &ledgerRef, evidenceHead, ledger, nil
}

func (s *Store) nextTaskEvidenceHeads(
	previous []EvidenceSubjectHead,
	record TransitionRecord,
	refs []EvidenceObjectRef,
) ([]EvidenceSubjectHead, error) {
	return s.nextEvidenceSubjectHeads(previous, refs, "task-index-revision", func(ref EvidenceObjectRef) (string, *EvidenceObjectRef, uint64, error) {
		revision, err := s.LoadTaskIndexRevision(ref)
		if err != nil {
			return "", nil, 0, err
		}
		return taskEvidenceSubjectID(revision.TaskRef), revision.PreviousRevision, revision.ControllerGeneration, nil
	}, record)
}

func (s *Store) nextEpisodeEvidenceHeads(
	previous []EvidenceSubjectHead,
	record TransitionRecord,
	refs []EvidenceObjectRef,
) ([]EvidenceSubjectHead, error) {
	return s.nextEvidenceSubjectHeads(previous, refs, "episode-index-revision", func(ref EvidenceObjectRef) (string, *EvidenceObjectRef, uint64, error) {
		revision, err := s.LoadEpisodeIndexRevision(ref)
		if err != nil {
			return "", nil, 0, err
		}
		return revision.EpisodeID, revision.PreviousRevision, revision.ControllerGeneration, nil
	}, record)
}

func (s *Store) nextEvidenceSubjectHeads(
	previous []EvidenceSubjectHead,
	refs []EvidenceObjectRef,
	kind string,
	load func(EvidenceObjectRef) (string, *EvidenceObjectRef, uint64, error),
	record TransitionRecord,
) ([]EvidenceSubjectHead, error) {
	bySubject := make(map[string]EvidenceObjectRef, len(previous)+len(refs))
	for _, head := range previous {
		if _, exists := bySubject[head.SubjectID]; exists {
			return nil, &EvidenceIntegrityError{Digest: head.RevisionRef.Digest, Reason: "published evidence head contains duplicate subject"}
		}
		bySubject[head.SubjectID] = head.RevisionRef
	}
	seen := make(map[string]bool, len(refs))
	for _, ref := range refs {
		if err := validateTypedEvidenceRef(ref, kind); err != nil {
			return nil, err
		}
		subject, previousRef, generation, err := load(ref)
		if err != nil {
			return nil, err
		}
		if seen[subject] {
			return nil, &EvidenceIntegrityError{Digest: ref.Digest, Reason: "evidence publication contains duplicate subject revision"}
		}
		seen[subject] = true
		if generation != record.CommittedGeneration {
			return nil, &EvidenceIntegrityError{Digest: ref.Digest, Reason: "evidence index revision is bound to the wrong controller generation"}
		}
		current, exists := bySubject[subject]
		switch {
		case exists && previousRef == nil:
			return nil, &EvidenceIntegrityError{Digest: ref.Digest, Reason: "evidence index revision does not link to current subject head"}
		case exists && !evidenceRefsEqual(current, *previousRef):
			return nil, &EvidenceIntegrityError{Digest: ref.Digest, Reason: "evidence index previous revision does not match current subject head"}
		case !exists && previousRef != nil:
			return nil, &EvidenceIntegrityError{Digest: ref.Digest, Reason: "first published evidence index revision unexpectedly has a predecessor"}
		}
		bySubject[subject] = ref
	}
	result := make([]EvidenceSubjectHead, 0, len(bySubject))
	for subject, ref := range bySubject {
		result = append(result, EvidenceSubjectHead{SubjectID: subject, RevisionRef: ref})
	}
	return canonicalEvidenceHeads(result), nil
}

func (s *Store) validatePublicationAttemptSeals(record TransitionRecord, refs []EvidenceObjectRef) error {
	seen := make(map[string]bool, len(refs))
	for _, ref := range refs {
		seal, err := s.LoadAttemptSeal(ref)
		if err != nil {
			return err
		}
		if seen[seal.AttemptSealID] {
			return &EvidenceIntegrityError{Digest: ref.Digest, Reason: "evidence publication contains duplicate attempt seal"}
		}
		seen[seal.AttemptSealID] = true
		if seal.SealingTransitionID != record.TransitionID {
			return &EvidenceIntegrityError{Digest: ref.Digest, Reason: "attempt seal is bound to a different transition"}
		}
	}
	return nil
}

func (s *Store) validatePublicationFinalizations(record TransitionRecord, refs []EvidenceObjectRef) error {
	seen := make(map[string]bool, len(refs))
	for _, ref := range refs {
		finalization, err := s.LoadAttemptFinalization(ref)
		if err != nil {
			return err
		}
		if seen[finalization.FinalizationID] {
			return &EvidenceIntegrityError{Digest: ref.Digest, Reason: "evidence publication contains duplicate attempt finalization"}
		}
		seen[finalization.FinalizationID] = true
		if finalization.TransitionID != record.TransitionID || finalization.ProjectSnapshotID != record.ProjectSnapshotNew {
			return &EvidenceIntegrityError{Digest: ref.Digest, Reason: "attempt finalization is bound to different transition authority"}
		}
	}
	return nil
}
