package controller

func (w *evidenceGraphWalker) validateLedgerTransitionDelta(
	ledgerRef EvidenceObjectRef,
	ledger EvidenceLedgerRecord,
	head EvidenceHead,
) error {
	previous := EvidenceHead{}
	if head.PreviousHead != nil {
		loaded, err := w.store.LoadEvidenceHead(*head.PreviousHead)
		if err != nil {
			return err
		}
		previous = loaded
	}
	expectedTasks, err := changedEvidenceHeads(previous.TaskHeads, head.TaskHeads)
	if err != nil {
		return evidenceGraphError(ledgerRef, err.Error())
	}
	if !evidenceHeadListsEqual(expectedTasks, ledger.ChangedTaskIndexHeads) {
		return evidenceGraphError(ledgerRef, "ledger changed task heads do not match evidence head transition")
	}
	expectedEpisodes, err := changedEvidenceHeads(previous.EpisodeHeads, head.EpisodeHeads)
	if err != nil {
		return evidenceGraphError(ledgerRef, err.Error())
	}
	if !evidenceHeadListsEqual(expectedEpisodes, ledger.ChangedEpisodeIndexHeads) {
		return evidenceGraphError(ledgerRef, "ledger changed episode heads do not match evidence head transition")
	}
	if err := w.validateLedgerChangedTaskAuthority(ledger); err != nil {
		return err
	}
	if err := w.validateLedgerChangedEpisodeAuthority(ledger); err != nil {
		return err
	}
	return w.validateLedgerAddedRecords(ledger)
}

func changedEvidenceHeads(previous, current []EvidenceSubjectHead) ([]EvidenceSubjectHead, error) {
	before := make(map[string]EvidenceObjectRef, len(previous))
	for _, head := range previous {
		if _, exists := before[head.SubjectID]; exists {
			return nil, &EvidenceIntegrityError{Digest: head.RevisionRef.Digest, Reason: "previous evidence head contains duplicate subject"}
		}
		before[head.SubjectID] = head.RevisionRef
	}
	seen := make(map[string]bool, len(current))
	changed := make([]EvidenceSubjectHead, 0, len(current))
	for _, head := range current {
		if seen[head.SubjectID] {
			return nil, &EvidenceIntegrityError{Digest: head.RevisionRef.Digest, Reason: "current evidence head contains duplicate subject"}
		}
		seen[head.SubjectID] = true
		old, exists := before[head.SubjectID]
		if !exists || !evidenceRefsEqual(old, head.RevisionRef) {
			changed = append(changed, head)
		}
	}
	for subject, ref := range before {
		if !seen[subject] {
			return nil, &EvidenceIntegrityError{Digest: ref.Digest, Reason: "evidence head transition removed published subject"}
		}
	}
	return canonicalEvidenceHeads(changed), nil
}

func evidenceHeadListsEqual(left, right []EvidenceSubjectHead) bool {
	left = canonicalEvidenceHeads(left)
	right = canonicalEvidenceHeads(right)
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].SubjectID != right[index].SubjectID || !evidenceRefsEqual(left[index].RevisionRef, right[index].RevisionRef) {
			return false
		}
	}
	return true
}

func (w *evidenceGraphWalker) validateLedgerChangedTaskAuthority(ledger EvidenceLedgerRecord) error {
	for _, head := range ledger.ChangedTaskIndexHeads {
		revision, err := w.store.LoadTaskIndexRevision(head.RevisionRef)
		if err != nil {
			return err
		}
		if taskEvidenceSubjectID(revision.TaskRef) != head.SubjectID || revision.ControllerGeneration != ledger.ControllerGeneration || revision.TransitionID != ledger.TransitionID {
			return evidenceGraphError(head.RevisionRef, "ledger changed task head authority is inconsistent")
		}
	}
	return nil
}

func (w *evidenceGraphWalker) validateLedgerChangedEpisodeAuthority(ledger EvidenceLedgerRecord) error {
	for _, head := range ledger.ChangedEpisodeIndexHeads {
		revision, err := w.store.LoadEpisodeIndexRevision(head.RevisionRef)
		if err != nil {
			return err
		}
		if revision.EpisodeID != head.SubjectID || revision.ControllerGeneration != ledger.ControllerGeneration || revision.TransitionID != ledger.TransitionID {
			return evidenceGraphError(head.RevisionRef, "ledger changed episode head authority is inconsistent")
		}
	}
	return nil
}

func (w *evidenceGraphWalker) validateLedgerAddedRecords(ledger EvidenceLedgerRecord) error {
	for _, ref := range ledger.AttemptSealsAdded {
		seal, err := w.walkAttemptSeal(ref)
		if err != nil {
			return err
		}
		if seal.ControllerGeneration != ledger.ControllerGeneration || seal.SealingTransitionID != ledger.TransitionID {
			return evidenceGraphError(ref, "ledger attempt seal authority is inconsistent")
		}
	}
	for _, ref := range ledger.FinalizationRecordsAdded {
		if err := w.addRef(ref); err != nil {
			return err
		}
		finalization, err := w.store.LoadAttemptFinalization(ref)
		if err != nil {
			return err
		}
		if finalization.ControllerGeneration != ledger.ControllerGeneration || finalization.TransitionID != ledger.TransitionID || finalization.ProjectSnapshotID != ledger.ProjectSnapshotID {
			return evidenceGraphError(ref, "ledger finalization authority is inconsistent")
		}
	}
	for _, ref := range ledger.FindingRecordsAdded {
		if err := w.addRef(ref); err != nil {
			return err
		}
	}
	return nil
}
