package controller

type EvidencePublicationInput struct {
	TaskRevisionRefs    []EvidenceObjectRef `json:"task_revision_refs,omitempty"`
	EpisodeRevisionRefs []EvidenceObjectRef `json:"episode_revision_refs,omitempty"`
	AttemptSealRefs     []EvidenceObjectRef `json:"attempt_seal_refs,omitempty"`
	FinalizationRefs    []EvidenceObjectRef `json:"finalization_refs,omitempty"`
}

type EvidencePublicationResult struct {
	EvidenceHeadRef EvidenceObjectRef     `json:"evidence_head_ref"`
	LedgerRecordRef EvidenceObjectRef     `json:"ledger_record_ref"`
	EvidenceHead    EvidenceHead          `json:"evidence_head"`
	LedgerRecord    EvidenceLedgerRecord  `json:"ledger_record"`
}
