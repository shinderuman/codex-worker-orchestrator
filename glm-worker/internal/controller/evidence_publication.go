package controller

import "fmt"

func (s *Store) commitAuthorityTransitionWithEvidenceLocked(
	record TransitionRecord,
	actual map[string]string,
	input EvidencePublicationInput,
	mutate func(*RepositoryControllerHead) error,
) (RepositoryControllerHead, EvidencePublicationResult, error) {
	if !hasEvidencePublicationInput(input) {
		committed, err := s.commitAuthorityTransitionCoreLocked(record, actual, false, mutate)
		return committed, EvidencePublicationResult{}, err
	}
	current, err := s.LoadHead()
	if err != nil {
		return RepositoryControllerHead{}, EvidencePublicationResult{}, err
	}
	if err := s.validateEvidencePublicationAuthority(current, record); err != nil {
		return RepositoryControllerHead{}, EvidencePublicationResult{}, err
	}
	materialized, err := s.materializeEvidencePublicationInput(input)
	if err != nil {
		return RepositoryControllerHead{}, EvidencePublicationResult{}, err
	}
	publication, err := s.prepareEvidencePublication(current, record, materialized)
	if err != nil {
		return RepositoryControllerHead{}, EvidencePublicationResult{}, err
	}
	committed, err := s.commitAuthorityTransitionCoreLocked(record, actual, false, func(next *RepositoryControllerHead) error {
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

func hasEvidencePublicationInput(input EvidencePublicationInput) bool {
	return len(input.AttemptSeals) != 0 || len(input.Finalizations) != 0 || len(input.TaskRevisions) != 0 || len(input.EpisodeRevisions) != 0 ||
		len(input.AttemptSealRefs) != 0 || len(input.FinalizationRefs) != 0 || len(input.TaskRevisionRefs) != 0 || len(input.EpisodeRevisionRefs) != 0
}

func (s *Store) validateEvidencePublicationAuthority(head RepositoryControllerHead, record TransitionRecord) error {
	if record.TransitionID == "" || record.CommittedGeneration == 0 || record.ProjectSnapshotNew == "" {
		return fmt.Errorf("evidence publication transition authority is incomplete")
	}
	if head.RepositoryIdentity != s.identity.LineageID || head.PendingTransitionID != record.TransitionID || head.ControllerGeneration != record.PreparedGeneration {
		return fmt.Errorf("evidence publication controller authority is stale")
	}
	return nil
}

func (s *Store) materializeEvidencePublicationInput(input EvidencePublicationInput) (EvidencePublicationInput, error) {
	result := input
	result.AttemptSealRefs = append([]EvidenceObjectRef(nil), input.AttemptSealRefs...)
	result.FinalizationRefs = append([]EvidenceObjectRef(nil), input.FinalizationRefs...)
	result.TaskRevisionRefs = append([]EvidenceObjectRef(nil), input.TaskRevisionRefs...)
	result.EpisodeRevisionRefs = append([]EvidenceObjectRef(nil), input.EpisodeRevisionRefs...)
	for _, record := range input.AttemptSeals {
		ref, _, err := s.StoreAttemptSeal(record)
		if err != nil {
			return EvidencePublicationInput{}, err
		}
		result.AttemptSealRefs = append(result.AttemptSealRefs, ref)
	}
	for _, record := range input.Finalizations {
		ref, _, err := s.StoreAttemptFinalization(record)
		if err != nil {
			return EvidencePublicationInput{}, err
		}
		result.FinalizationRefs = append(result.FinalizationRefs, ref)
	}
	for _, record := range input.TaskRevisions {
		ref, _, err := s.StoreTaskIndexRevision(record)
		if err != nil {
			return EvidencePublicationInput{}, err
		}
		result.TaskRevisionRefs = append(result.TaskRevisionRefs, ref)
	}
	for _, record := range input.EpisodeRevisions {
		ref, _, err := s.StoreEpisodeIndexRevision(record)
		if err != nil {
			return EvidencePublicationInput{}, err
		}
		result.EpisodeRevisionRefs = append(result.EpisodeRevisionRefs, ref)
	}
	result.AttemptSealRefs = canonicalEvidenceRefs(result.AttemptSealRefs)
	result.FinalizationRefs = canonicalEvidenceRefs(result.FinalizationRefs)
	result.TaskRevisionRefs = canonicalEvidenceRefs(result.TaskRevisionRefs)
	result.EpisodeRevisionRefs = canonicalEvidenceRefs(result.EpisodeRevisionRefs)
	return result, nil
}

func (s *Store) prepareEvidencePublication(
	controllerHead RepositoryControllerHead,
	record TransitionRecord,
	input EvidencePublicationInput,
) (EvidencePublicationResult, error) {
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
	if err := s.validatePublicationRefs(record, input); err != nil {
		return EvidencePublicationResult{}, err
	}
	headRef, storedHead, err := s.storeNextEvidenceHead(previousHeadRef, taskHeads, episodeHeads, record)
	if err != nil {
		return EvidencePublicationResult{}, err
	}
	ledgerRef, storedLedger, err := s.storeNextEvidenceLedger(previousLedgerRef, controllerHead, record, headRef)
	if err != nil {
		return EvidencePublicationResult{}, err
	}
	if previousLedgerRef != nil && storedLedger.Sequence != previousLedger.Sequence+1 {
		return EvidencePublicationResult{}, &EvidenceIntegrityError{Digest: ledgerRef.Digest, Reason: "evidence ledger sequence did not advance exactly once"}
	}
	graphDigest, err := s.validateEvidenceGraphRoots(ledgerRef, headRef, storedLedger.Sequence)
	if err != nil {
		return EvidencePublicationResult{}, err
	}
	return EvidencePublicationResult{
		EvidenceHeadRef:     headRef,
		LedgerRecordRef:     ledgerRef,
		EvidenceHead:        storedHead,
		LedgerRecord:        storedLedger,
		EvidenceGraphDigest: graphDigest,
	}, nil
}

func (s *Store) storeNextEvidenceHead(
	previous *EvidenceObjectRef,
	taskHeads []EvidenceSubjectHead,
	episodeHeads []EvidenceSubjectHead,
	record TransitionRecord,
) (EvidenceObjectRef, EvidenceHead, error) {
	return s.StoreEvidenceHead(EvidenceHead{
		SchemaVersion:        evidenceSchemaVersion,
		RepositoryIdentity:   s.identity.LineageID,
		PreviousHead:         previous,
		TaskHeads:            taskHeads,
		EpisodeHeads:         episodeHeads,
		ControllerGeneration: record.CommittedGeneration,
		ProjectSnapshotID:    record.ProjectSnapshotNew,
	})
}

func (s *Store) storeNextEvidenceLedger(
	previous *EvidenceObjectRef,
	head RepositoryControllerHead,
	record TransitionRecord,
	evidenceHeadRef EvidenceObjectRef,
) (EvidenceObjectRef, EvidenceLedgerRecord, error) {
	return s.StoreEvidenceLedgerRecord(EvidenceLedgerRecord{
		SchemaVersion:        evidenceSchemaVersion,
		Sequence:             head.EvidenceLedgerSequence + 1,
		PreviousRecord:       previous,
		RepositoryIdentity:   s.identity.LineageID,
		ControllerGeneration: record.CommittedGeneration,
		TransitionID:         record.TransitionID,
		ProjectSnapshotID:    record.ProjectSnapshotNew,
		EvidenceHeadRef:      evidenceHeadRef,
	})
}

func (s *Store) validatePublicationRefs(record TransitionRecord, input EvidencePublicationInput) error {
	if err := s.validatePublicationAttemptSeals(record, input.AttemptSealRefs); err != nil {
		return err
	}
	return s.validatePublicationFinalizations(record, input.FinalizationRefs)
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
	return nextEvidenceSubjectHeads(previous, refs, "task-index-revision", func(ref EvidenceObjectRef) (string, *EvidenceObjectRef, uint64, error) {
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
	return nextEvidenceSubjectHeads(previous, refs, "episode-index-revision", func(ref EvidenceObjectRef) (string, *EvidenceObjectRef, uint64, error) {
		revision, err := s.LoadEpisodeIndexRevision(ref)
		if err != nil {
			return "", nil, 0, err
		}
		return revision.EpisodeID, revision.PreviousRevision, revision.ControllerGeneration, nil
	}, record)
}

func nextEvidenceSubjectHeads(
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
		if err := applyEvidenceSubjectRevision(bySubject, seen, ref, kind, load, record); err != nil {
			return nil, err
		}
	}
	result := make([]EvidenceSubjectHead, 0, len(bySubject))
	for subject, ref := range bySubject {
		result = append(result, EvidenceSubjectHead{SubjectID: subject, RevisionRef: ref})
	}
	return canonicalEvidenceHeads(result), nil
}

func applyEvidenceSubjectRevision(
	bySubject map[string]EvidenceObjectRef,
	seen map[string]bool,
	ref EvidenceObjectRef,
	kind string,
	load func(EvidenceObjectRef) (string, *EvidenceObjectRef, uint64, error),
	record TransitionRecord,
) error {
	if err := validateTypedEvidenceRef(ref, kind); err != nil {
		return err
	}
	subject, previousRef, generation, err := load(ref)
	if err != nil {
		return err
	}
	if seen[subject] {
		return &EvidenceIntegrityError{Digest: ref.Digest, Reason: "evidence publication contains duplicate subject revision"}
	}
	seen[subject] = true
	if generation != record.CommittedGeneration {
		return &EvidenceIntegrityError{Digest: ref.Digest, Reason: "evidence index revision is bound to the wrong controller generation"}
	}
	current, exists := bySubject[subject]
	if err := validateEvidenceSubjectPredecessor(ref, current, exists, previousRef); err != nil {
		return err
	}
	bySubject[subject] = ref
	return nil
}

func validateEvidenceSubjectPredecessor(ref, current EvidenceObjectRef, exists bool, previous *EvidenceObjectRef) error {
	switch {
	case exists && previous == nil:
		return &EvidenceIntegrityError{Digest: ref.Digest, Reason: "evidence index revision does not link to current subject head"}
	case exists && !evidenceRefsEqual(current, *previous):
		return &EvidenceIntegrityError{Digest: ref.Digest, Reason: "evidence index previous revision does not match current subject head"}
	case !exists && previous != nil:
		return &EvidenceIntegrityError{Digest: ref.Digest, Reason: "first published evidence index revision unexpectedly has a predecessor"}
	default:
		return nil
	}
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
