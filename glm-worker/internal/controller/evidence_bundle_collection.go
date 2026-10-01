package controller

import "errors"

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
	return c.addRefs(seal.EvidenceRefs)
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
		if err := c.addRefs(record.EvidenceRefs); err != nil {
			return err
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
		if err := c.addMatchingFinalizations(revision.Finalizations, sealRef); err != nil {
			return err
		}
		if revision.PreviousRevision == nil {
			return nil
		}
		current = *revision.PreviousRevision
	}
}

func (c *evidenceBundleCollector) addMatchingFinalizations(refs []EvidenceObjectRef, sealRef EvidenceObjectRef) error {
	for _, ref := range refs {
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
	return nil
}

func (c *evidenceBundleCollector) addTaskRevision(ref EvidenceObjectRef) error {
	if err := c.addRef(ref); err != nil {
		return err
	}
	revision, err := c.store.LoadTaskIndexRevision(ref)
	if err != nil {
		return err
	}
	return c.addRevisionEvidence(
		revision.AttemptSeals,
		revision.Finalizations,
		revision.TerminalRecord,
		revision.FindingRecords,
		revision.DependencyEdgeRecords,
		revision.PublicationLineageRecords,
	)
}

func (c *evidenceBundleCollector) addTaskRevisionChain(root EvidenceObjectRef) error {
	current := root
	for {
		if err := c.addTaskRevision(current); err != nil {
			return err
		}
		revision, err := c.store.LoadTaskIndexRevision(current)
		if err != nil {
			return err
		}
		if revision.PreviousRevision == nil {
			return nil
		}
		current = *revision.PreviousRevision
	}
}

func (c *evidenceBundleCollector) addEpisodeRevision(ref EvidenceObjectRef) error {
	if err := c.addRef(ref); err != nil {
		return err
	}
	revision, err := c.store.LoadEpisodeIndexRevision(ref)
	if err != nil {
		return err
	}
	return c.addRevisionEvidence(
		revision.AttemptSeals,
		revision.Finalizations,
		revision.CloseRecord,
		revision.FindingRecords,
		revision.TransitionRecords,
		revision.IntegrationHistory,
	)
}

func (c *evidenceBundleCollector) addEpisodeRevisionChain(root EvidenceObjectRef) error {
	current := root
	for {
		if err := c.addEpisodeRevision(current); err != nil {
			return err
		}
		revision, err := c.store.LoadEpisodeIndexRevision(current)
		if err != nil {
			return err
		}
		if revision.PreviousRevision == nil {
			return nil
		}
		current = *revision.PreviousRevision
	}
}

func (c *evidenceBundleCollector) addRevisionEvidence(
	seals []EvidenceObjectRef,
	finalizations []EvidenceObjectRef,
	terminal *EvidenceObjectRef,
	groups ...[]EvidenceObjectRef,
) error {
	if err := c.addIndexEvidence(seals, finalizations); err != nil {
		return err
	}
	for _, refs := range groups {
		if err := c.addRefs(refs); err != nil {
			return err
		}
	}
	if terminal != nil {
		return c.addRef(*terminal)
	}
	return nil
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

func (c *evidenceBundleCollector) addRefs(refs []EvidenceObjectRef) error {
	for _, ref := range refs {
		if err := c.addRef(ref); err != nil {
			return err
		}
	}
	return nil
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
		SchemaVersion:        evidenceSchemaVersion,
		Kind:                 kind,
		RootRef:              root,
		RootIdentity:         identity,
		RepositoryIdentity:   publication.head.RepositoryIdentity,
		ControllerGeneration: publication.ledger.ControllerGeneration,
		ProjectSnapshotID:    publication.ledger.ProjectSnapshotID,
		PublicationLedgerRef: publication.ledgerRef,
		PublicationHeadRef:   publication.headRef,
		EvidenceGraphDigest:  digest,
		Objects:              objects,
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
