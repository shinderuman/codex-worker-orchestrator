package controller

import (
	"errors"
	"fmt"
	"sort"
)

const (
	evidenceBundleAttempt = "attempt"
	evidenceBundleTask    = "semantic-task"
	evidenceBundleEpisode = "episode"
)

type EvidenceBundleObject struct {
	Ref     EvidenceObjectRef `json:"ref"`
	Data    []byte            `json:"data,omitempty"`
	Missing bool              `json:"missing,omitempty"`
}

type EvidenceBundleProjection struct {
	SchemaVersion        int                    `json:"schema_version"`
	Kind                 string                 `json:"kind"`
	RootRef              EvidenceObjectRef      `json:"root_ref"`
	RootIdentity         string                 `json:"root_identity"`
	RepositoryIdentity   string                 `json:"repository_identity"`
	ControllerGeneration uint64                 `json:"controller_generation"`
	ProjectSnapshotID    string                 `json:"project_snapshot_id"`
	PublicationLedgerRef EvidenceObjectRef      `json:"publication_ledger_ref"`
	PublicationHeadRef   EvidenceObjectRef      `json:"publication_head_ref"`
	EvidenceGraphDigest  string                 `json:"evidence_graph_digest"`
	Objects              []EvidenceBundleObject `json:"objects"`
}

type evidenceBundlePublication struct {
	ledgerRef EvidenceObjectRef
	headRef   EvidenceObjectRef
	ledger    EvidenceLedgerRecord
	head      EvidenceHead
	proofRefs []EvidenceObjectRef
}

type evidenceBundleCollector struct {
	store *Store
	refs  map[string]EvidenceObjectRef
}

func (s *Store) BuildAttemptEvidenceBundle(sealRef EvidenceObjectRef) (EvidenceBundleProjection, error) {
	authority, err := s.loadEvidenceBundleAuthority()
	if err != nil {
		return EvidenceBundleProjection{}, err
	}
	seal, err := s.LoadAttemptSeal(sealRef)
	if err != nil {
		return EvidenceBundleProjection{}, err
	}
	taskRef, episodeRef, err := s.latestAttemptIndexRoots(authority.head, sealRef, seal)
	if err != nil {
		return EvidenceBundleProjection{}, err
	}
	publication, err := s.findEvidenceBundlePublication(authority, []EvidenceObjectRef{taskRef}, optionalEvidenceRefSlice(episodeRef))
	if err != nil {
		return EvidenceBundleProjection{}, err
	}
	collector := newEvidenceBundleCollector(s)
	if err := collector.addAttemptSeal(sealRef); err != nil {
		return EvidenceBundleProjection{}, err
	}
	if err := collector.addAttemptFinalizations(taskRef, sealRef); err != nil {
		return EvidenceBundleProjection{}, err
	}
	if err := collector.addRef(taskRef); err != nil {
		return EvidenceBundleProjection{}, err
	}
	if episodeRef != nil {
		if err := collector.addRef(*episodeRef); err != nil {
			return EvidenceBundleProjection{}, err
		}
	}
	return collector.finishBundle(evidenceBundleAttempt, sealRef, seal.AttemptID, publication)
}

func (s *Store) BuildTaskEvidenceBundle(rootRef EvidenceObjectRef) (EvidenceBundleProjection, error) {
	authority, err := s.loadEvidenceBundleAuthority()
	if err != nil {
		return EvidenceBundleProjection{}, err
	}
	root, err := s.LoadTaskIndexRevision(rootRef)
	if err != nil {
		return EvidenceBundleProjection{}, err
	}
	publication, err := s.findEvidenceBundlePublication(authority, []EvidenceObjectRef{rootRef}, nil)
	if err != nil {
		return EvidenceBundleProjection{}, err
	}
	collector := newEvidenceBundleCollector(s)
	if err := collector.addTaskRevisionChain(rootRef); err != nil {
		return EvidenceBundleProjection{}, err
	}
	return collector.finishBundle(evidenceBundleTask, rootRef, taskEvidenceSubjectID(root.TaskRef), publication)
}

func (s *Store) BuildEpisodeEvidenceBundle(rootRef EvidenceObjectRef) (EvidenceBundleProjection, error) {
	authority, err := s.loadEvidenceBundleAuthority()
	if err != nil {
		return EvidenceBundleProjection{}, err
	}
	root, err := s.LoadEpisodeIndexRevision(rootRef)
	if err != nil {
		return EvidenceBundleProjection{}, err
	}
	publication, err := s.findEvidenceBundlePublication(authority, nil, []EvidenceObjectRef{rootRef})
	if err != nil {
		return EvidenceBundleProjection{}, err
	}
	collector := newEvidenceBundleCollector(s)
	if err := collector.addEpisodeRevisionChain(rootRef); err != nil {
		return EvidenceBundleProjection{}, err
	}
	if err := collector.addEpisodeTaskHistory(publication.head, rootRef); err != nil {
		return EvidenceBundleProjection{}, err
	}
	return collector.finishBundle(evidenceBundleEpisode, rootRef, root.EpisodeID, publication)
}

type evidenceBundleAuthority struct {
	headRef   EvidenceObjectRef
	ledgerRef EvidenceObjectRef
	sequence  uint64
	head      EvidenceHead
}

func (s *Store) loadEvidenceBundleAuthority() (evidenceBundleAuthority, error) {
	head, err := s.LoadHead()
	if err != nil {
		return evidenceBundleAuthority{}, err
	}
	if head.EvidenceHeadRef == nil || head.EvidenceLedgerHeadRef == nil || head.EvidenceLedgerSequence == 0 {
		return evidenceBundleAuthority{}, fmt.Errorf("evidence Bundle requires complete evidence authority")
	}
	if _, err := s.validateEvidenceGraphRoots(*head.EvidenceLedgerHeadRef, *head.EvidenceHeadRef, head.EvidenceLedgerSequence); err != nil {
		return evidenceBundleAuthority{}, err
	}
	evidenceHead, err := s.LoadEvidenceHead(*head.EvidenceHeadRef)
	if err != nil {
		return evidenceBundleAuthority{}, err
	}
	return evidenceBundleAuthority{
		headRef: *head.EvidenceHeadRef, ledgerRef: *head.EvidenceLedgerHeadRef,
		sequence: head.EvidenceLedgerSequence, head: evidenceHead,
	}, nil
}

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
	match := -1
	for index := len(ledgers) - 1; index >= 0; index-- {
		ok, err := s.publicationContainsRoots(heads[index], taskRefs, episodeRefs)
		if err != nil {
			return evidenceBundlePublication{}, err
		}
		if ok {
			match = index
		} else if match >= 0 {
			break
		}
	}
	if match < 0 {
		return evidenceBundlePublication{}, fmt.Errorf("evidence Bundle root was never published by evidence ledger")
	}
	proof := append([]EvidenceObjectRef(nil), ledgerRefs[match:]...)
	return evidenceBundlePublication{
		ledgerRef: ledgerRefs[match], headRef: ledgers[match].EvidenceHeadRef,
		ledger: ledgers[match], head: heads[match], proofRefs: proof,
	}, nil
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
	for _, ref := range taskRefs {
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
	for _, ref := range episodeRefs {
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

func newEvidenceBundleCollector(store *Store) *evidenceBundleCollector {
	return &evidenceBundleCollector{store: store, refs: map[string]EvidenceObjectRef{}}
}

func (c *evidenceBundleCollector) addRef(ref EvidenceObjectRef) error {
	if err := validateEvidenceRef(ref); err != nil {
		return err
	}
	key := evidenceRefKey(ref)
	if existing, ok := c.refs[key]; ok {
		if !evidenceRefsEqual(existing, ref) {
			return evidenceGraphError(ref, "Bundle contains conflicting evidence reference metadata")
		}
		return nil
	}
	if _, err := c.store.LoadEvidenceObject(ref); err != nil {
		var integrity *EvidenceIntegrityError
		if !ref.Required && errors.As(err, &integrity) {
			c.refs[key] = ref
			return nil
		}
		return err
	}
	c.refs[key] = ref
	return nil
}

func (c *evidenceBundleCollector) addAttemptSeal(ref EvidenceObjectRef) error {
	if err := c.addRef(ref); err != nil {
		return err
	}
	seal, err := c.store.LoadAttemptSeal(ref)
	if err != nil {
		return err
	}
	if _, err := c.store.VerifyGitObjectArchive(seal.GitObjectArchive); err != nil {
		return err
	}
	if err := c.addRef(seal.GitObjectArchive); err != nil {
		return err
	}
	for _, evidenceRef := range seal.EvidenceRefs {
		if err := c.addRef(evidenceRef); err != nil {
			return err
		}
	}
	return nil
}

func (c *evidenceBundleCollector) addFinalization(ref EvidenceObjectRef) error {
	current := ref
	for {
		if err := c.addRef(current); err != nil {
			return err
		}
		record, err := c.store.LoadAttemptFinalization(current)
		if err != nil {
			return err
		}
		if err := c.addAttemptSeal(record.AttemptSealRef); err != nil {
			return err
		}
		for _, evidenceRef := range record.EvidenceRefs {
			if err := c.addRef(evidenceRef); err != nil {
				return err
			}
		}
		if record.PreviousFinalization == nil {
			return nil
		}
		current = *record.PreviousFinalization
	}
}

func (c *evidenceBundleCollector) addAttemptFinalizations(taskRoot, sealRef EvidenceObjectRef) error {
	current := taskRoot
	for {
		revision, err := c.store.LoadTaskIndexRevision(current)
		if err != nil {
			return err
		}
		for _, ref := range revision.Finalizations {
			record, err := c.store.LoadAttemptFinalization(ref)
			if err != nil {
				return err
			}
			if evidenceRefsEqual(record.AttemptSealRef, sealRef) {
				if err := c.addFinalization(ref); err != nil {
					return err
				}
			}
		}
		if revision.PreviousRevision == nil {
			return nil
		}
		current = *revision.PreviousRevision
	}
}

func (c *evidenceBundleCollector) addTaskRevisionChain(root EvidenceObjectRef) error {
	current := root
	for {
		if err := c.addRef(current); err != nil {
			return err
		}
		revision, err := c.store.LoadTaskIndexRevision(current)
		if err != nil {
			return err
		}
		if err := c.addIndexEvidence(revision.AttemptSeals, revision.Finalizations); err != nil {
			return err
		}
		if revision.PreviousRevision == nil {
			return nil
		}
		current = *revision.PreviousRevision
	}
}

func (c *evidenceBundleCollector) addEpisodeRevisionChain(root EvidenceObjectRef) error {
	current := root
	for {
		if err := c.addRef(current); err != nil {
			return err
		}
		revision, err := c.store.LoadEpisodeIndexRevision(current)
		if err != nil {
			return err
		}
		if err := c.addIndexEvidence(revision.AttemptSeals, revision.Finalizations); err != nil {
			return err
		}
		if revision.PreviousRevision == nil {
			return nil
		}
		current = *revision.PreviousRevision
	}
}

func (c *evidenceBundleCollector) addIndexEvidence(seals, finalizations []EvidenceObjectRef) error {
	for _, ref := range seals {
		if err := c.addAttemptSeal(ref); err != nil {
			return err
		}
	}
	for _, ref := range finalizations {
		if err := c.addFinalization(ref); err != nil {
			return err
		}
	}
	return nil
}

func (c *evidenceBundleCollector) addEpisodeTaskHistory(head EvidenceHead, episodeRoot EvidenceObjectRef) error {
	sealRefs, err := c.episodeSealRefs(episodeRoot)
	if err != nil {
		return err
	}
	subjects := map[string]bool{}
	for _, sealRef := range sealRefs {
		seal, err := c.store.LoadAttemptSeal(sealRef)
		if err != nil {
			return err
		}
		subjects[taskEvidenceSubjectID(seal.SemanticTaskRef)] = true
	}
	for subject := range subjects {
		taskHead, ok := evidenceSubjectHead(head.TaskHeads, subject)
		if !ok {
			return fmt.Errorf("episode Bundle task %s is absent from publication evidence head", subject)
		}
		if err := c.addTaskRevisionChain(taskHead.RevisionRef); err != nil {
			return err
		}
	}
	return nil
}

func (c *evidenceBundleCollector) episodeSealRefs(root EvidenceObjectRef) ([]EvidenceObjectRef, error) {
	seen := map[string]EvidenceObjectRef{}
	current := root
	for {
		revision, err := c.store.LoadEpisodeIndexRevision(current)
		if err != nil {
			return nil, err
		}
		for _, ref := range revision.AttemptSeals {
			seen[evidenceRefKey(ref)] = ref
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

func (c *evidenceBundleCollector) finishBundle(kind string, root EvidenceObjectRef, identity string, publication evidenceBundlePublication) (EvidenceBundleProjection, error) {
	if err := c.addRef(publication.headRef); err != nil {
		return EvidenceBundleProjection{}, err
	}
	for _, ref := range publication.proofRefs {
		if err := c.addRef(ref); err != nil {
			return EvidenceBundleProjection{}, err
		}
	}
	objects, digest, err := c.materialize()
	if err != nil {
		return EvidenceBundleProjection{}, err
	}
	return EvidenceBundleProjection{
		SchemaVersion: evidenceSchemaVersion, Kind: kind, RootRef: root, RootIdentity: identity,
		RepositoryIdentity: publication.head.RepositoryIdentity,
		ControllerGeneration: publication.ledger.ControllerGeneration,
		ProjectSnapshotID: publication.ledger.ProjectSnapshotID,
		PublicationLedgerRef: publication.ledgerRef, PublicationHeadRef: publication.headRef,
		EvidenceGraphDigest: digest, Objects: objects,
	}, nil
}

func (c *evidenceBundleCollector) materialize() ([]EvidenceBundleObject, string, error) {
	refs := make([]EvidenceObjectRef, 0, len(c.refs))
	for _, ref := range c.refs {
		refs = append(refs, ref)
	}
	refs = canonicalEvidenceRefs(refs)
	objects := make([]EvidenceBundleObject, 0, len(refs))
	for _, ref := range refs {
		data, err := c.store.LoadEvidenceObject(ref)
		if err != nil {
			var integrity *EvidenceIntegrityError
			if !ref.Required && errors.As(err, &integrity) {
				objects = append(objects, EvidenceBundleObject{Ref: ref, Missing: true})
				continue
			}
			return nil, "", err
		}
		objects = append(objects, EvidenceBundleObject{Ref: ref, Data: data})
	}
	digest, err := evidenceGraphDigest(c.refs)
	return objects, digest, err
}

func optionalEvidenceRefSlice(ref *EvidenceObjectRef) []EvidenceObjectRef {
	if ref == nil {
		return nil
	}
	return []EvidenceObjectRef{*ref}
}

func canonicalEvidenceBundleObjects(objects []EvidenceBundleObject) []EvidenceBundleObject {
	result := append([]EvidenceBundleObject(nil), objects...)
	sort.Slice(result, func(i, j int) bool {
		return evidenceRefKey(result[i].Ref) < evidenceRefKey(result[j].Ref)
	})
	return result
}
