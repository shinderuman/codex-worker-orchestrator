package controller

type TerminalTaskInput struct {
	ExpectedGeneration uint64          `json:"expected_generation"`
	ProjectSnapshotID  string          `json:"project_snapshot_id"`
	CandidateID        string          `json:"candidate_id"`
	TaskRef            SemanticTaskRef `json:"task_ref"`
}

type MetadataTaskBinding struct {
	Source SemanticTaskRef `json:"source"`
	Result SemanticTaskRef `json:"result"`
}

type TerminalTaskRecord struct {
	SchemaVersion        int                   `json:"schema_version"`
	RepositoryIdentity   string                `json:"repository_identity"`
	TransitionID         string                `json:"transition_id"`
	ControllerGeneration uint64                `json:"controller_generation"`
	TaskRef              SemanticTaskRef       `json:"task_ref"`
	AttemptID            string                `json:"attempt_id"`
	CandidateRef         EvidenceObjectRef     `json:"candidate_ref"`
	CandidateOID         string                `json:"candidate_oid"`
	SourceProject        ProjectSnapshot       `json:"source_project"`
	ResultProject        ProjectSnapshot       `json:"result_project"`
	Bindings             []MetadataTaskBinding `json:"bindings"`
	GitArchive           EvidenceObjectRef     `json:"git_archive"`
	Previous             *EvidenceObjectRef    `json:"previous,omitempty"`
}

type TerminalMetadataFile struct {
	Path     string `json:"path"`
	Mode     string `json:"mode"`
	OldOID   string `json:"old_oid"`
	NewOID   string `json:"new_oid"`
	OldBytes []byte `json:"old_bytes"`
	NewBytes []byte `json:"new_bytes,omitempty"`
}

type TerminalMetadataOperation struct {
	Record      TerminalTaskRecord      `json:"record"`
	RecordRef   EvidenceObjectRef       `json:"record_ref"`
	Policy      PublicationPolicy       `json:"policy"`
	Files       []TerminalMetadataFile  `json:"files"`
	RootTaskRef *SemanticTaskRef        `json:"root_task_ref,omitempty"`
	Episode     *BlockerEpisodeRevision `json:"episode,omitempty"`
}

const terminalRetire = "RETIRE_TERMINAL_TASK_METADATA"
