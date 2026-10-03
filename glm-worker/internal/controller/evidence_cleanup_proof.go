package controller

import "fmt"

type CleanupDurabilityProof struct {
	RepositoryIdentity     string             `json:"repository_identity"`
	ControllerGeneration   uint64             `json:"controller_generation"`
	ProjectSnapshotID      string             `json:"project_snapshot_id"`
	EvidenceHeadRef        EvidenceObjectRef  `json:"evidence_head_ref"`
	EvidenceLedgerHeadRef  EvidenceObjectRef  `json:"evidence_ledger_head_ref"`
	EvidenceLedgerSequence uint64             `json:"evidence_ledger_sequence"`
	EvidenceGraphDigest    string             `json:"evidence_graph_digest"`
	AttemptSealRef         EvidenceObjectRef  `json:"attempt_seal_ref"`
	AttemptSealID          string             `json:"attempt_seal_id"`
	AttemptID              string             `json:"attempt_id"`
	SemanticTaskRef        SemanticTaskRef    `json:"semantic_task_ref"`
	TaskSubjectID          string             `json:"task_subject_id"`
	TaskHeadRef            EvidenceObjectRef  `json:"task_head_ref"`
	TaskRevisionRef        EvidenceObjectRef  `json:"task_revision_ref"`
	EpisodeID              string             `json:"episode_id,omitempty"`
	EpisodeHeadRef         *EvidenceObjectRef `json:"episode_head_ref,omitempty"`
	EpisodeRevisionRef     *EvidenceObjectRef `json:"episode_revision_ref,omitempty"`
}

type cleanupEvidenceAuthority struct {
	head        RepositoryControllerHead
	evidence    EvidenceHead
	graphDigest string
}

func (s *Store) ProveCleanupDurability(sealRef EvidenceObjectRef) (CleanupDurabilityProof, error) {
	authority, err := s.loadCleanupEvidenceAuthority()
	if err != nil {
		return CleanupDurabilityProof{}, err
	}
	seal, err := s.LoadAttemptSeal(sealRef)
	if err != nil {
		return CleanupDurabilityProof{}, err
	}
	proof, err := s.buildCleanupTaskProof(authority, sealRef, seal)
	if err != nil {
		return CleanupDurabilityProof{}, err
	}
	if seal.EpisodeID == "" {
		return proof, nil
	}
	return s.addCleanupEpisodeProof(authority.evidence, sealRef, seal, proof)
}

func (s *Store) loadCleanupEvidenceAuthority() (cleanupEvidenceAuthority, error) {
	head, err := s.LoadHead()
	if err != nil {
		return cleanupEvidenceAuthority{}, err
	}
	if head.EvidenceHeadRef == nil || head.EvidenceLedgerHeadRef == nil || head.EvidenceLedgerSequence == 0 {
		return cleanupEvidenceAuthority{}, fmt.Errorf("cleanup durability proof requires complete evidence authority")
	}
	graphDigest, err := s.validateEvidenceGraphRoots(*head.EvidenceLedgerHeadRef, *head.EvidenceHeadRef, head.EvidenceLedgerSequence)
	if err != nil {
		return cleanupEvidenceAuthority{}, err
	}
	evidenceHead, err := s.LoadEvidenceHead(*head.EvidenceHeadRef)
	if err != nil {
		return cleanupEvidenceAuthority{}, err
	}
	return cleanupEvidenceAuthority{head: head, evidence: evidenceHead, graphDigest: graphDigest}, nil
}

func (s *Store) buildCleanupTaskProof(
	authority cleanupEvidenceAuthority,
	sealRef EvidenceObjectRef,
	seal AttemptSeal,
) (CleanupDurabilityProof, error) {
	taskSubject := taskEvidenceSubjectID(seal.SemanticTaskRef)
	taskHead, ok := evidenceSubjectHead(authority.evidence.TaskHeads, taskSubject)
	if !ok {
		return CleanupDurabilityProof{}, evidenceGraphError(sealRef, "attempt seal is not reachable from current task evidence head")
	}
	taskRevision, err := s.findTaskRevisionContainingSeal(taskHead.RevisionRef, sealRef)
	if err != nil {
		return CleanupDurabilityProof{}, err
	}
	return CleanupDurabilityProof{
		RepositoryIdentity:     authority.head.RepositoryIdentity,
		ControllerGeneration:   authority.head.ControllerGeneration,
		ProjectSnapshotID:      authority.head.ProjectSnapshotID,
		EvidenceHeadRef:        *authority.head.EvidenceHeadRef,
		EvidenceLedgerHeadRef:  *authority.head.EvidenceLedgerHeadRef,
		EvidenceLedgerSequence: authority.head.EvidenceLedgerSequence,
		EvidenceGraphDigest:    authority.graphDigest,
		AttemptSealRef:         sealRef,
		AttemptSealID:          seal.AttemptSealID,
		AttemptID:              seal.AttemptID,
		SemanticTaskRef:        seal.SemanticTaskRef,
		TaskSubjectID:          taskSubject,
		TaskHeadRef:            taskHead.RevisionRef,
		TaskRevisionRef:        taskRevision,
		EpisodeID:              seal.EpisodeID,
	}, nil
}

func (s *Store) addCleanupEpisodeProof(
	evidenceHead EvidenceHead,
	sealRef EvidenceObjectRef,
	seal AttemptSeal,
	proof CleanupDurabilityProof,
) (CleanupDurabilityProof, error) {
	episodeHead, ok := evidenceSubjectHead(evidenceHead.EpisodeHeads, seal.EpisodeID)
	if !ok {
		return CleanupDurabilityProof{}, evidenceGraphError(sealRef, "attempt seal is not reachable from current episode evidence head")
	}
	episodeRevision, err := s.findEpisodeRevisionContainingSeal(episodeHead.RevisionRef, sealRef)
	if err != nil {
		return CleanupDurabilityProof{}, err
	}
	proof.EpisodeHeadRef = evidenceRefPointer(episodeHead.RevisionRef)
	proof.EpisodeRevisionRef = evidenceRefPointer(episodeRevision)
	return proof, nil
}

func evidenceSubjectHead(heads []EvidenceSubjectHead, subject string) (EvidenceSubjectHead, bool) {
	for _, head := range heads {
		if head.SubjectID == subject {
			return head, true
		}
	}
	return EvidenceSubjectHead{}, false
}

func (s *Store) findTaskRevisionContainingSeal(headRef, sealRef EvidenceObjectRef) (EvidenceObjectRef, error) {
	current := headRef
	for {
		revision, err := s.LoadTaskIndexRevision(current)
		if err != nil {
			return EvidenceObjectRef{}, err
		}
		if evidenceRefSliceContains(revision.AttemptSeals, sealRef) {
			return current, nil
		}
		if revision.PreviousRevision == nil {
			return EvidenceObjectRef{}, evidenceGraphError(sealRef, "attempt seal is not reachable from current task index chain")
		}
		current = *revision.PreviousRevision
	}
}

func (s *Store) findEpisodeRevisionContainingSeal(headRef, sealRef EvidenceObjectRef) (EvidenceObjectRef, error) {
	current := headRef
	for {
		revision, err := s.LoadEpisodeIndexRevision(current)
		if err != nil {
			return EvidenceObjectRef{}, err
		}
		if evidenceRefSliceContains(revision.AttemptSeals, sealRef) {
			return current, nil
		}
		if revision.PreviousRevision == nil {
			return EvidenceObjectRef{}, evidenceGraphError(sealRef, "attempt seal is not reachable from current episode index chain")
		}
		current = *revision.PreviousRevision
	}
}

func evidenceRefSliceContains(refs []EvidenceObjectRef, target EvidenceObjectRef) bool {
	for _, ref := range refs {
		if evidenceRefsEqual(ref, target) {
			return true
		}
	}
	return false
}

func evidenceRefPointer(ref EvidenceObjectRef) *EvidenceObjectRef {
	value := ref
	return &value
}
