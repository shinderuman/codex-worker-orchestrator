package controller

func (s *Store) buildEvidenceLedgerRecord(
	previous *EvidenceObjectRef,
	head RepositoryControllerHead,
	record TransitionRecord,
	evidenceHeadRef EvidenceObjectRef,
	input EvidencePublicationInput,
) (EvidenceLedgerRecord, error) {
	changedTaskHeads, err := s.ledgerTaskHeadChanges(input.TaskRevisionRefs)
	if err != nil {
		return EvidenceLedgerRecord{}, err
	}
	changedEpisodeHeads, err := s.ledgerEpisodeHeadChanges(input.EpisodeRevisionRefs)
	if err != nil {
		return EvidenceLedgerRecord{}, err
	}
	return EvidenceLedgerRecord{
		SchemaVersion:            evidenceSchemaVersion,
		Sequence:                 head.EvidenceLedgerSequence + 1,
		PreviousRecord:           previous,
		RepositoryIdentity:       s.identity.LineageID,
		ControllerGeneration:     record.CommittedGeneration,
		TransitionID:             record.TransitionID,
		ChangedTaskIndexHeads:    changedTaskHeads,
		ChangedEpisodeIndexHeads: changedEpisodeHeads,
		AttemptSealsAdded:        append([]EvidenceObjectRef(nil), input.AttemptSealRefs...),
		FinalizationRecordsAdded: append([]EvidenceObjectRef(nil), input.FinalizationRefs...),
		FindingRecordsAdded:      append([]EvidenceObjectRef(nil), input.FindingRecordRefs...),
		ProjectSnapshotID:        record.ProjectSnapshotNew,
		EvidenceHeadRef:          evidenceHeadRef,
	}, nil
}

func (s *Store) ledgerTaskHeadChanges(refs []EvidenceObjectRef) ([]EvidenceSubjectHead, error) {
	result := make([]EvidenceSubjectHead, 0, len(refs))
	for _, ref := range refs {
		revision, err := s.LoadTaskIndexRevision(ref)
		if err != nil {
			return nil, err
		}
		result = append(result, EvidenceSubjectHead{
			SubjectID:   taskEvidenceSubjectID(revision.TaskRef),
			RevisionRef: ref,
		})
	}
	return canonicalEvidenceHeads(result), nil
}

func (s *Store) ledgerEpisodeHeadChanges(refs []EvidenceObjectRef) ([]EvidenceSubjectHead, error) {
	result := make([]EvidenceSubjectHead, 0, len(refs))
	for _, ref := range refs {
		revision, err := s.LoadEpisodeIndexRevision(ref)
		if err != nil {
			return nil, err
		}
		result = append(result, EvidenceSubjectHead{
			SubjectID:   revision.EpisodeID,
			RevisionRef: ref,
		})
	}
	return canonicalEvidenceHeads(result), nil
}

func (s *Store) validatePublicationFindingRecords(refs []EvidenceObjectRef) error {
	if err := validateUniqueTypedEvidenceRefs(refs, "finding-record"); err != nil {
		return err
	}
	for _, ref := range refs {
		if _, err := s.LoadEvidenceObject(ref); err != nil {
			return err
		}
	}
	return nil
}
