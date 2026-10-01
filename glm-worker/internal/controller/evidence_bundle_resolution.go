package controller

import "fmt"

func (s *Store) latestAttemptIndexRoots(head EvidenceHead, sealRef EvidenceObjectRef, seal AttemptSeal) (EvidenceObjectRef, *EvidenceObjectRef, error) {
	taskHead, ok := evidenceSubjectHead(head.TaskHeads, taskEvidenceSubjectID(seal.SemanticTaskRef))
	if !ok {
		return EvidenceObjectRef{}, nil, evidenceGraphError(sealRef, "attempt Bundle seal is outside current task evidence authority")
	}
	taskRef, err := s.latestTaskRevisionForAttempt(taskHead.RevisionRef, sealRef)
	if err != nil {
		return EvidenceObjectRef{}, nil, err
	}
	if seal.EpisodeID == "" {
		return taskRef, nil, nil
	}
	episodeHead, ok := evidenceSubjectHead(head.EpisodeHeads, seal.EpisodeID)
	if !ok {
		return EvidenceObjectRef{}, nil, evidenceGraphError(sealRef, "attempt Bundle seal is outside current episode evidence authority")
	}
	episodeRef, err := s.latestEpisodeRevisionForAttempt(episodeHead.RevisionRef, sealRef)
	if err != nil {
		return EvidenceObjectRef{}, nil, err
	}
	return taskRef, evidenceRefPointer(episodeRef), nil
}

func (s *Store) latestTaskRevisionForAttempt(headRef, sealRef EvidenceObjectRef) (EvidenceObjectRef, error) {
	current := headRef
	for {
		revision, err := s.LoadTaskIndexRevision(current)
		if err != nil {
			return EvidenceObjectRef{}, err
		}
		relevant, err := s.taskRevisionReferencesAttempt(revision, sealRef)
		if err != nil {
			return EvidenceObjectRef{}, err
		}
		if relevant {
			return current, nil
		}
		if revision.PreviousRevision == nil {
			return EvidenceObjectRef{}, evidenceGraphError(sealRef, "attempt Bundle seal is absent from task revision history")
		}
		current = *revision.PreviousRevision
	}
}

func (s *Store) latestEpisodeRevisionForAttempt(headRef, sealRef EvidenceObjectRef) (EvidenceObjectRef, error) {
	current := headRef
	for {
		revision, err := s.LoadEpisodeIndexRevision(current)
		if err != nil {
			return EvidenceObjectRef{}, err
		}
		relevant, err := s.episodeRevisionReferencesAttempt(revision, sealRef)
		if err != nil {
			return EvidenceObjectRef{}, err
		}
		if relevant {
			return current, nil
		}
		if revision.PreviousRevision == nil {
			return EvidenceObjectRef{}, evidenceGraphError(sealRef, "attempt Bundle seal is absent from episode revision history")
		}
		current = *revision.PreviousRevision
	}
}

func (s *Store) taskRevisionReferencesAttempt(revision TaskIndexRevision, sealRef EvidenceObjectRef) (bool, error) {
	if evidenceRefSliceContains(revision.AttemptSeals, sealRef) {
		return true, nil
	}
	return s.finalizationRefsContainAttempt(revision.Finalizations, sealRef)
}

func (s *Store) episodeRevisionReferencesAttempt(revision EpisodeIndexRevision, sealRef EvidenceObjectRef) (bool, error) {
	if evidenceRefSliceContains(revision.AttemptSeals, sealRef) {
		return true, nil
	}
	return s.finalizationRefsContainAttempt(revision.Finalizations, sealRef)
}

func (s *Store) finalizationRefsContainAttempt(refs []EvidenceObjectRef, sealRef EvidenceObjectRef) (bool, error) {
	for _, ref := range refs {
		record, err := s.LoadAttemptFinalization(ref)
		if err != nil {
			return false, err
		}
		if evidenceRefsEqual(record.AttemptSealRef, sealRef) {
			return true, nil
		}
	}
	return false, nil
}

func (s *Store) findEvidenceBundlePublication(
	authority evidenceBundleAuthority,
	taskRefs []EvidenceObjectRef,
	episodeRefs []EvidenceObjectRef,
) (evidenceBundlePublication, error) {
	ledgerRefs, ledgers, heads, err := s.loadEvidencePublicationHistory(authority)
	if err != nil {
		return evidenceBundlePublication{}, err
	}
	for index := len(ledgers) - 1; index >= 0; index-- {
		ok, err := s.publicationContainsRoots(heads[index], taskRefs, episodeRefs)
		if err != nil {
			return evidenceBundlePublication{}, err
		}
		if ok {
			proof := append([]EvidenceObjectRef(nil), ledgerRefs[index:]...)
			return evidenceBundlePublication{
				ledgerRef: ledgerRefs[index],
				headRef:   ledgers[index].EvidenceHeadRef,
				ledger:    ledgers[index],
				head:      heads[index],
				proofRefs: proof,
			}, nil
		}
	}
	return evidenceBundlePublication{}, fmt.Errorf("evidence Bundle root was never published by evidence ledger")
}

func (s *Store) loadEvidencePublicationHistory(authority evidenceBundleAuthority) ([]EvidenceObjectRef, []EvidenceLedgerRecord, []EvidenceHead, error) {
	var refs []EvidenceObjectRef
	var ledgers []EvidenceLedgerRecord
	var heads []EvidenceHead
	current := authority.ledgerRef
	for {
		ledger, err := s.LoadEvidenceLedgerRecord(current)
		if err != nil {
			return nil, nil, nil, err
		}
		head, err := s.LoadEvidenceHead(ledger.EvidenceHeadRef)
		if err != nil {
			return nil, nil, nil, err
		}
		refs = append(refs, current)
		ledgers = append(ledgers, ledger)
		heads = append(heads, head)
		if ledger.PreviousRecord == nil {
			return refs, ledgers, heads, nil
		}
		current = *ledger.PreviousRecord
	}
}

func (s *Store) publicationContainsRoots(head EvidenceHead, taskRefs, episodeRefs []EvidenceObjectRef) (bool, error) {
	tasksOK, err := s.publicationContainsTaskRoots(head, taskRefs)
	if err != nil || !tasksOK {
		return tasksOK, err
	}
	return s.publicationContainsEpisodeRoots(head, episodeRefs)
}

func (s *Store) publicationContainsTaskRoots(head EvidenceHead, refs []EvidenceObjectRef) (bool, error) {
	for _, ref := range refs {
		revision, err := s.LoadTaskIndexRevision(ref)
		if err != nil {
			return false, err
		}
		subject, ok := evidenceSubjectHead(head.TaskHeads, taskEvidenceSubjectID(revision.TaskRef))
		if !ok {
			return false, nil
		}
		contains, err := s.taskRevisionChainContains(subject.RevisionRef, ref)
		if err != nil || !contains {
			return false, err
		}
	}
	return true, nil
}

func (s *Store) publicationContainsEpisodeRoots(head EvidenceHead, refs []EvidenceObjectRef) (bool, error) {
	for _, ref := range refs {
		revision, err := s.LoadEpisodeIndexRevision(ref)
		if err != nil {
			return false, err
		}
		subject, ok := evidenceSubjectHead(head.EpisodeHeads, revision.EpisodeID)
		if !ok {
			return false, nil
		}
		contains, err := s.episodeRevisionChainContains(subject.RevisionRef, ref)
		if err != nil || !contains {
			return false, err
		}
	}
	return true, nil
}

func (s *Store) taskRevisionChainContains(headRef, target EvidenceObjectRef) (bool, error) {
	current := headRef
	for {
		if evidenceRefsEqual(current, target) {
			return true, nil
		}
		revision, err := s.LoadTaskIndexRevision(current)
		if err != nil {
			return false, err
		}
		if revision.PreviousRevision == nil {
			return false, nil
		}
		current = *revision.PreviousRevision
	}
}

func (s *Store) episodeRevisionChainContains(headRef, target EvidenceObjectRef) (bool, error) {
	current := headRef
	for {
		if evidenceRefsEqual(current, target) {
			return true, nil
		}
		revision, err := s.LoadEpisodeIndexRevision(current)
		if err != nil {
			return false, err
		}
		if revision.PreviousRevision == nil {
			return false, nil
		}
		current = *revision.PreviousRevision
	}
}

func (s *Store) episodeTaskIndexRefs(root EvidenceObjectRef) ([]EvidenceObjectRef, error) {
	seen := map[string]EvidenceObjectRef{}
	current := root
	for {
		revision, err := s.LoadEpisodeIndexRevision(current)
		if err != nil {
			return nil, err
		}
		for _, head := range revision.TaskIndexHeads {
			seen[evidenceRefKey(head.RevisionRef)] = head.RevisionRef
		}
		if revision.PreviousRevision == nil {
			break
		}
		current = *revision.PreviousRevision
	}
	refs := make([]EvidenceObjectRef, 0, len(seen))
	for _, ref := range seen {
		refs = append(refs, ref)
	}
	return canonicalEvidenceRefs(refs), nil
}
