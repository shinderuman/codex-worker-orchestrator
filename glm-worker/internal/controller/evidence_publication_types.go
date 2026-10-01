package controller

type EvidencePublicationInput struct {
	AttemptSeals     []AttemptSeal               `json:"attempt_seals,omitempty"`
	Finalizations    []AttemptFinalizationRecord `json:"finalizations,omitempty"`
	TaskRevisions    []TaskIndexRevision         `json:"task_revisions,omitempty"`
	EpisodeRevisions []EpisodeIndexRevision      `json:"episode_revisions,omitempty"`

	AttemptSealRefs     []EvidenceObjectRef `json:"attempt_seal_refs,omitempty"`
	FinalizationRefs    []EvidenceObjectRef `json:"finalization_refs,omitempty"`
	FindingRecordRefs   []EvidenceObjectRef `json:"finding_record_refs,omitempty"`
	TaskRevisionRefs    []EvidenceObjectRef `json:"task_revision_refs,omitempty"`
	EpisodeRevisionRefs []EvidenceObjectRef `json:"episode_revision_refs,omitempty"`
}

type EvidencePublicationResult struct {
	EvidenceHeadRef     EvidenceObjectRef    `json:"evidence_head_ref"`
	LedgerRecordRef     EvidenceObjectRef    `json:"ledger_record_ref"`
	EvidenceHead        EvidenceHead         `json:"evidence_head"`
	LedgerRecord        EvidenceLedgerRecord `json:"ledger_record"`
	EvidenceGraphDigest string               `json:"evidence_graph_digest"`
}
