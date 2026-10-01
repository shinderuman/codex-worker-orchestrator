package controller

import "time"

type EvidencePublicationHead struct {
	SchemaVersion        int               `json:"schema_version"`
	PublicationDigest    string            `json:"publication_digest"`
	RepositoryIdentity   string            `json:"repository_identity"`
	Sequence             uint64            `json:"sequence"`
	EvidenceHeadRef      EvidenceObjectRef `json:"evidence_head_ref"`
	LedgerRecordRef      EvidenceObjectRef `json:"ledger_record_ref"`
	ControllerGeneration uint64            `json:"controller_generation"`
	TransitionID         string            `json:"transition_id"`
	ProjectSnapshotID    string            `json:"project_snapshot_id"`
	UpdatedAt            time.Time         `json:"updated_at"`
}

type EvidencePublicationInput struct {
	ExpectedPublicationDigest string
	ExpectedControllerGeneration uint64
	TransitionID string
	ProjectSnapshotID string
	TaskRevisionRefs []EvidenceObjectRef
	EpisodeRevisionRefs []EvidenceObjectRef
}

type EvidencePublicationResult struct {
	Publication EvidencePublicationHead
	EvidenceHead EvidenceHead
	LedgerRecord EvidenceLedgerRecord
}
