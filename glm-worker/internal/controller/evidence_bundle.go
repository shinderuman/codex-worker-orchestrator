package controller

import "fmt"

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

type evidenceBundleAuthority struct {
	headRef   EvidenceObjectRef
	ledgerRef EvidenceObjectRef
	sequence  uint64
	head      EvidenceHead
}

const (
	evidenceBundleAttempt = "attempt"
	evidenceBundleTask    = "semantic-task"
	evidenceBundleEpisode = "episode"
)

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
	if err := collector.addTaskRevision(taskRef); err != nil {
		return EvidenceBundleProjection{}, err
	}
	if episodeRef != nil {
		if err := collector.addEpisodeRevision(*episodeRef); err != nil {
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
	taskRefs, err := s.episodeTaskIndexRefs(rootRef)
	if err != nil {
		return EvidenceBundleProjection{}, err
	}
	publication, err := s.findEvidenceBundlePublication(authority, taskRefs, []EvidenceObjectRef{rootRef})
	if err != nil {
		return EvidenceBundleProjection{}, err
	}
	collector := newEvidenceBundleCollector(s)
	if err := collector.addEpisodeRevisionChain(rootRef); err != nil {
		return EvidenceBundleProjection{}, err
	}
	for _, taskRef := range taskRefs {
		if err := collector.addTaskRevisionChain(taskRef); err != nil {
			return EvidenceBundleProjection{}, err
		}
	}
	return collector.finishBundle(evidenceBundleEpisode, rootRef, root.EpisodeID, publication)
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
		headRef:   *head.EvidenceHeadRef,
		ledgerRef: *head.EvidenceLedgerHeadRef,
		sequence:  head.EvidenceLedgerSequence,
		head:      evidenceHead,
	}, nil
}
