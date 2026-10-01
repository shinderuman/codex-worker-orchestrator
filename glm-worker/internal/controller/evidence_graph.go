package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type evidenceGraphWalker struct {
	store    *Store
	refs     map[string]EvidenceObjectRef
	expanded map[string]bool
}

func (s *Store) validateEvidenceGraphRoots(
	ledgerRef EvidenceObjectRef,
	headRef EvidenceObjectRef,
	sequence uint64,
) (string, error) {
	walker := evidenceGraphWalker{
		store:    s,
		refs:     map[string]EvidenceObjectRef{},
		expanded: map[string]bool{},
	}
	if err := walker.walkLedgerChain(ledgerRef, headRef, sequence); err != nil {
		return "", err
	}
	return evidenceGraphDigest(walker.refs)
}

func (w *evidenceGraphWalker) walkLedgerChain(
	ledgerRef EvidenceObjectRef,
	headRef EvidenceObjectRef,
	sequence uint64,
) error {
	currentLedger := ledgerRef
	expectedHead := headRef
	expectedSequence := sequence
	for {
		ledger, head, err := w.loadLedgerStep(currentLedger, expectedHead, expectedSequence)
		if err != nil {
			return err
		}
		nextLedger, nextHead, nextSequence, done, err := w.advanceLedgerChain(currentLedger, ledger, head, expectedSequence)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
		currentLedger = nextLedger
		expectedHead = nextHead
		expectedSequence = nextSequence
	}
}

func (w *evidenceGraphWalker) advanceLedgerChain(
	currentLedger EvidenceObjectRef,
	ledger EvidenceLedgerRecord,
	head EvidenceHead,
	expectedSequence uint64,
) (EvidenceObjectRef, EvidenceObjectRef, uint64, bool, error) {
	if ledger.PreviousRecord == nil {
		if expectedSequence != 1 || head.PreviousHead != nil {
			return EvidenceObjectRef{}, EvidenceObjectRef{}, 0, false, evidenceGraphError(currentLedger, "evidence ledger root linkage is inconsistent")
		}
		return EvidenceObjectRef{}, EvidenceObjectRef{}, 0, true, nil
	}
	if expectedSequence <= 1 {
		return EvidenceObjectRef{}, EvidenceObjectRef{}, 0, false, evidenceGraphError(currentLedger, "evidence ledger sequence underflow")
	}
	previous, err := w.store.LoadEvidenceLedgerRecord(*ledger.PreviousRecord)
	if err != nil {
		return EvidenceObjectRef{}, EvidenceObjectRef{}, 0, false, err
	}
	if head.PreviousHead == nil || !evidenceRefsEqual(*head.PreviousHead, previous.EvidenceHeadRef) {
		return EvidenceObjectRef{}, EvidenceObjectRef{}, 0, false, evidenceGraphError(currentLedger, "evidence head chain does not match ledger predecessor")
	}
	return *ledger.PreviousRecord, previous.EvidenceHeadRef, expectedSequence - 1, false, nil
}

func (w *evidenceGraphWalker) loadLedgerStep(
	ledgerRef EvidenceObjectRef,
	headRef EvidenceObjectRef,
	sequence uint64,
) (EvidenceLedgerRecord, EvidenceHead, error) {
	if err := w.addRef(ledgerRef); err != nil {
		return EvidenceLedgerRecord{}, EvidenceHead{}, err
	}
	ledger, err := w.store.LoadEvidenceLedgerRecord(ledgerRef)
	if err != nil {
		return EvidenceLedgerRecord{}, EvidenceHead{}, err
	}
	if ledger.Sequence != sequence || !evidenceRefsEqual(ledger.EvidenceHeadRef, headRef) {
		return EvidenceLedgerRecord{}, EvidenceHead{}, evidenceGraphError(ledgerRef, "evidence ledger sequence or head linkage is inconsistent")
	}
	head, err := w.walkEvidenceHead(headRef, ledger)
	if err != nil {
		return EvidenceLedgerRecord{}, EvidenceHead{}, err
	}
	return ledger, head, nil
}

func (w *evidenceGraphWalker) walkEvidenceHead(
	ref EvidenceObjectRef,
	ledger EvidenceLedgerRecord,
) (EvidenceHead, error) {
	if err := w.addRef(ref); err != nil {
		return EvidenceHead{}, err
	}
	head, err := w.store.LoadEvidenceHead(ref)
	if err != nil {
		return EvidenceHead{}, err
	}
	if head.ControllerGeneration != ledger.ControllerGeneration || head.ProjectSnapshotID != ledger.ProjectSnapshotID {
		return EvidenceHead{}, evidenceGraphError(ref, "evidence head authority does not match ledger authority")
	}
	if err := w.walkTaskHeads(head.TaskHeads, head.ControllerGeneration); err != nil {
		return EvidenceHead{}, err
	}
	if err := w.walkEpisodeHeads(head.EpisodeHeads, head.ControllerGeneration); err != nil {
		return EvidenceHead{}, err
	}
	return head, nil
}

func (w *evidenceGraphWalker) walkTaskHeads(heads []EvidenceSubjectHead, maxGeneration uint64) error {
	seen := map[string]bool{}
	for _, head := range heads {
		if seen[head.SubjectID] {
			return evidenceGraphError(head.RevisionRef, "evidence task head contains duplicate subject")
		}
		seen[head.SubjectID] = true
		if err := w.walkTaskRevisionChain(head.SubjectID, head.RevisionRef, maxGeneration); err != nil {
			return err
		}
	}
	return nil
}

func (w *evidenceGraphWalker) walkEpisodeHeads(heads []EvidenceSubjectHead, maxGeneration uint64) error {
	seen := map[string]bool{}
	for _, head := range heads {
		if seen[head.SubjectID] {
			return evidenceGraphError(head.RevisionRef, "evidence episode head contains duplicate subject")
		}
		seen[head.SubjectID] = true
		if err := w.walkEpisodeRevisionChain(head.SubjectID, head.RevisionRef, maxGeneration); err != nil {
			return err
		}
	}
	return nil
}

func (w *evidenceGraphWalker) walkTaskRevisionChain(
	subject string,
	ref EvidenceObjectRef,
	maxGeneration uint64,
) error {
	current := ref
	upper := maxGeneration + 1
	for {
		if err := w.addRef(current); err != nil {
			return err
		}
		revision, err := w.store.LoadTaskIndexRevision(current)
		if err != nil {
			return err
		}
		if taskEvidenceSubjectID(revision.TaskRef) != subject || revision.ControllerGeneration >= upper {
			return evidenceGraphError(current, "task index revision chain authority is inconsistent")
		}
		if err := w.walkTaskRevisionEvidence(subject, revision); err != nil {
			return err
		}
		if revision.PreviousRevision == nil {
			return nil
		}
		upper = revision.ControllerGeneration
		current = *revision.PreviousRevision
	}
}

func (w *evidenceGraphWalker) walkEpisodeRevisionChain(
	subject string,
	ref EvidenceObjectRef,
	maxGeneration uint64,
) error {
	current := ref
	upper := maxGeneration + 1
	for {
		if err := w.addRef(current); err != nil {
			return err
		}
		revision, err := w.store.LoadEpisodeIndexRevision(current)
		if err != nil {
			return err
		}
		if revision.EpisodeID != subject || revision.ControllerGeneration >= upper {
			return evidenceGraphError(current, "episode index revision chain authority is inconsistent")
		}
		if err := w.walkEpisodeRevisionEvidence(subject, revision); err != nil {
			return err
		}
		if revision.PreviousRevision == nil {
			return nil
		}
		upper = revision.ControllerGeneration
		current = *revision.PreviousRevision
	}
}

func (w *evidenceGraphWalker) walkTaskRevisionEvidence(subject string, revision TaskIndexRevision) error {
	match := func(seal AttemptSeal) bool {
		return taskEvidenceSubjectID(seal.SemanticTaskRef) == subject
	}
	if err := w.walkIndexEvidence(revision.AttemptSeals, revision.Finalizations, match); err != nil {
		return err
	}
	if err := w.addRefs(revision.FindingRecords); err != nil {
		return err
	}
	if err := w.addRefs(revision.DependencyEdgeRecords); err != nil {
		return err
	}
	if err := w.addRefs(revision.PublicationLineageRecords); err != nil {
		return err
	}
	if revision.TerminalRecord != nil {
		return w.addRef(*revision.TerminalRecord)
	}
	return nil
}

func (w *evidenceGraphWalker) walkEpisodeRevisionEvidence(subject string, revision EpisodeIndexRevision) error {
	match := func(seal AttemptSeal) bool {
		return seal.EpisodeID == subject
	}
	if err := w.walkIndexEvidence(revision.AttemptSeals, revision.Finalizations, match); err != nil {
		return err
	}
	if err := w.walkEpisodeTaskIndexHeads(revision); err != nil {
		return err
	}
	if err := w.addRefs(revision.FindingRecords); err != nil {
		return err
	}
	if err := w.addRefs(revision.TransitionRecords); err != nil {
		return err
	}
	if err := w.addRefs(revision.IntegrationHistory); err != nil {
		return err
	}
	if revision.CloseRecord != nil {
		return w.addRef(*revision.CloseRecord)
	}
	return nil
}

func (w *evidenceGraphWalker) walkEpisodeTaskIndexHeads(revision EpisodeIndexRevision) error {
	for _, head := range revision.TaskIndexHeads {
		if err := w.walkTaskRevisionChain(head.SubjectID, head.RevisionRef, revision.ControllerGeneration); err != nil {
			return err
		}
	}
	return nil
}

func (w *evidenceGraphWalker) walkIndexEvidence(
	seals []EvidenceObjectRef,
	finalizations []EvidenceObjectRef,
	match func(AttemptSeal) bool,
) error {
	for _, ref := range seals {
		seal, err := w.walkAttemptSeal(ref)
		if err != nil {
			return err
		}
		if !match(seal) {
			return evidenceGraphError(ref, "attempt seal subject does not match index subject")
		}
	}
	for _, ref := range finalizations {
		if err := w.walkFinalizationChain(ref, match); err != nil {
			return err
		}
	}
	return nil
}

func (w *evidenceGraphWalker) walkFinalizationChain(
	ref EvidenceObjectRef,
	match func(AttemptSeal) bool,
) error {
	current := ref
	for {
		if err := w.addRef(current); err != nil {
			return err
		}
		record, err := w.store.LoadAttemptFinalization(current)
		if err != nil {
			return err
		}
		seal, err := w.walkAttemptSeal(record.AttemptSealRef)
		if err != nil {
			return err
		}
		if !match(seal) {
			return evidenceGraphError(current, "attempt finalization subject does not match index subject")
		}
		if err := w.addRefs(record.EvidenceRefs); err != nil {
			return err
		}
		if record.PreviousFinalization == nil {
			return nil
		}
		current = *record.PreviousFinalization
	}
}

func (w *evidenceGraphWalker) walkAttemptSeal(ref EvidenceObjectRef) (AttemptSeal, error) {
	if err := w.addRef(ref); err != nil {
		return AttemptSeal{}, err
	}
	seal, err := w.store.LoadAttemptSeal(ref)
	if err != nil {
		return AttemptSeal{}, err
	}
	if w.expanded[evidenceRefKey(ref)] {
		return seal, nil
	}
	w.expanded[evidenceRefKey(ref)] = true
	if err := w.addRef(seal.GitObjectArchive); err != nil {
		return AttemptSeal{}, err
	}
	if err := w.addRefs(seal.EvidenceRefs); err != nil {
		return AttemptSeal{}, err
	}
	return seal, nil
}

func (w *evidenceGraphWalker) addRefs(refs []EvidenceObjectRef) error {
	for _, ref := range refs {
		if err := w.addRef(ref); err != nil {
			return err
		}
	}
	return nil
}

func (w *evidenceGraphWalker) addRef(ref EvidenceObjectRef) error {
	if err := validateEvidenceRef(ref); err != nil {
		return err
	}
	key := evidenceRefKey(ref)
	if existing, ok := w.refs[key]; ok {
		if !evidenceRefsEqual(existing, ref) {
			return evidenceGraphError(ref, "evidence graph contains conflicting reference metadata")
		}
		return nil
	}
	if _, err := w.store.LoadEvidenceObject(ref); err != nil {
		var integrity *EvidenceIntegrityError
		if !ref.Required && errors.As(err, &integrity) && strings.Contains(integrity.Reason, "missing") {
			w.refs[key] = ref
			return nil
		}
		return err
	}
	w.refs[key] = ref
	return nil
}

func evidenceGraphDigest(refs map[string]EvidenceObjectRef) (string, error) {
	values := make([]EvidenceObjectRef, 0, len(refs))
	for _, ref := range refs {
		values = append(values, ref)
	}
	values = canonicalEvidenceRefs(values)
	data, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return digestStrings("controller-evidence-graph-v1", string(data)), nil
}

func evidenceRefKey(ref EvidenceObjectRef) string {
	return fmt.Sprintf("%s\x00%s\x00%s\x00%d\x00%t", ref.Kind, ref.LogicalIdentity, ref.Digest, ref.Length, ref.Required)
}

func evidenceGraphError(ref EvidenceObjectRef, reason string) error {
	return &EvidenceIntegrityError{Digest: ref.Digest, Reason: reason}
}
