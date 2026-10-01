package controller

import "time"

type EvidenceObjectRef struct {
	Digest          string `json:"digest"`
	Kind            string `json:"kind"`
	MediaType       string `json:"media_type"`
	Length          int64  `json:"length"`
	LogicalIdentity string `json:"logical_identity"`
	Required        bool   `json:"required"`
}

type AttemptSeal struct {
	SchemaVersion         int                 `json:"schema_version"`
	AttemptSealID         string              `json:"attempt_seal_id"`
	RepositoryIdentity    string              `json:"repository_identity"`
	SemanticTaskRef       SemanticTaskRef     `json:"semantic_task_ref"`
	RootTaskRef           SemanticTaskRef     `json:"root_task_ref"`
	AttemptID             string              `json:"attempt_id"`
	PredecessorAttemptID  string              `json:"predecessor_attempt_id,omitempty"`
	EpisodeID             string              `json:"episode_id,omitempty"`
	EpisodeRevision       uint64              `json:"episode_revision,omitempty"`
	ControllerGeneration  uint64              `json:"controller_generation"`
	SealingTransitionID   string              `json:"sealing_transition_id"`
	RevokedLeaseID        string              `json:"revoked_lease_id"`
	WorkspaceID           string              `json:"workspace_id"`
	ExecutionPurpose      string              `json:"execution_purpose"`
	StartedAt             time.Time           `json:"started_at"`
	SealedAt              time.Time           `json:"sealed_at"`
	Disposition           string              `json:"disposition"`
	ExecutionBaseOID      string              `json:"execution_base_oid"`
	BaselineIndexTree     string              `json:"baseline_index_tree"`
	BaselineWorktreeTree  string              `json:"baseline_worktree_tree"`
	CurrentIndexTree      string              `json:"current_index_tree"`
	CurrentWorktreeTree   string              `json:"current_worktree_tree"`
	ParentAuthorityDigest string              `json:"parent_authority_digest"`
	CandidateID           string              `json:"candidate_id,omitempty"`
	GitObjectArchive      EvidenceObjectRef   `json:"git_object_archive"`
	EvidenceRefs          []EvidenceObjectRef `json:"evidence_refs,omitempty"`
	Coverage              string              `json:"coverage"`
	Missing               []string            `json:"missing,omitempty"`
	Unreadable            []string            `json:"unreadable,omitempty"`
}

type AttemptFinalizationRecord struct {
	SchemaVersion        int                 `json:"schema_version"`
	FinalizationID       string              `json:"finalization_id"`
	AttemptSealRef       EvidenceObjectRef   `json:"attempt_seal_ref"`
	PreviousFinalization *EvidenceObjectRef  `json:"previous_finalization,omitempty"`
	ControllerGeneration uint64              `json:"controller_generation"`
	TransitionID         string              `json:"transition_id"`
	ProjectSnapshotID    string              `json:"project_snapshot_id"`
	Kind                 string              `json:"kind"`
	EvidenceRefs         []EvidenceObjectRef `json:"evidence_refs,omitempty"`
	CreatedAt            time.Time           `json:"created_at"`
}

type TaskIndexRevision struct {
	SchemaVersion        int                 `json:"schema_version"`
	RevisionID           string              `json:"revision_id"`
	TaskRef              SemanticTaskRef     `json:"task_ref"`
	PreviousRevision     *EvidenceObjectRef  `json:"previous_revision,omitempty"`
	AttemptSeals         []EvidenceObjectRef `json:"attempt_seals"`
	Finalizations        []EvidenceObjectRef `json:"finalizations,omitempty"`
	Terminal             bool                `json:"terminal"`
	ControllerGeneration uint64              `json:"controller_generation"`
	CreatedAt            time.Time           `json:"created_at"`
}

type EpisodeIndexRevision struct {
	SchemaVersion        int                 `json:"schema_version"`
	RevisionID           string              `json:"revision_id"`
	EpisodeID            string              `json:"episode_id"`
	EpisodeRevision      uint64              `json:"episode_revision"`
	PreviousRevision     *EvidenceObjectRef  `json:"previous_revision,omitempty"`
	AttemptSeals         []EvidenceObjectRef `json:"attempt_seals"`
	Finalizations        []EvidenceObjectRef `json:"finalizations,omitempty"`
	Closed               bool                `json:"closed"`
	ControllerGeneration uint64              `json:"controller_generation"`
	CreatedAt            time.Time           `json:"created_at"`
}

type EvidenceSubjectHead struct {
	SubjectID   string            `json:"subject_id"`
	RevisionRef EvidenceObjectRef `json:"revision_ref"`
}

type EvidenceHead struct {
	SchemaVersion        int                   `json:"schema_version"`
	HeadDigest           string                `json:"head_digest"`
	RepositoryIdentity   string                `json:"repository_identity"`
	PreviousHead         *EvidenceObjectRef    `json:"previous_head,omitempty"`
	TaskHeads            []EvidenceSubjectHead `json:"task_heads,omitempty"`
	EpisodeHeads         []EvidenceSubjectHead `json:"episode_heads,omitempty"`
	ControllerGeneration uint64                `json:"controller_generation"`
	ProjectSnapshotID    string                `json:"project_snapshot_id"`
	CreatedAt            time.Time             `json:"created_at"`
}

type EvidenceLedgerRecord struct {
	SchemaVersion        int                `json:"schema_version"`
	RecordDigest         string             `json:"record_digest"`
	Sequence             uint64             `json:"sequence"`
	PreviousRecord       *EvidenceObjectRef `json:"previous_record,omitempty"`
	RepositoryIdentity   string             `json:"repository_identity"`
	ControllerGeneration uint64             `json:"controller_generation"`
	TransitionID         string             `json:"transition_id"`
	ProjectSnapshotID    string             `json:"project_snapshot_id"`
	EvidenceHeadRef      EvidenceObjectRef  `json:"evidence_head_ref"`
	CreatedAt            time.Time          `json:"created_at"`
}

type EvidenceIntegrityError struct {
	Digest string
	Reason string
}

func (e *EvidenceIntegrityError) Error() string {
	return "evidence integrity failure for " + e.Digest + ": " + e.Reason
}
