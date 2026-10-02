package controller

import "time"

type PublicationPolicy struct {
	Remote         string `json:"remote"`
	RemoteRef      string `json:"remote_ref"`
	LocalRef       string `json:"local_ref"`
	RequireInstall bool   `json:"require_install"`
}

type CandidateEvidence struct {
	SchemaVersion      int               `json:"schema_version"`
	RepositoryIdentity string            `json:"repository_identity"`
	AttemptID          string            `json:"attempt_id"`
	SnapshotID         string            `json:"snapshot_id"`
	CandidateID        string            `json:"candidate_id,omitempty"`
	BaseOID            string            `json:"base_oid"`
	TreeOID            string            `json:"tree_oid"`
	Kind               string            `json:"kind"`
	Result             string            `json:"result"`
	Artifact           EvidenceObjectRef `json:"artifact"`
}

type AcceptedCandidate struct {
	SchemaVersion        int                 `json:"schema_version"`
	RevisionID           string              `json:"revision_id"`
	CandidateID          string              `json:"candidate_id"`
	Previous             *EvidenceObjectRef  `json:"previous,omitempty"`
	AttemptID            string              `json:"attempt_id"`
	TaskRef              SemanticTaskRef     `json:"task_ref"`
	RootTaskRef          SemanticTaskRef     `json:"root_task_ref"`
	EpisodeID            string              `json:"episode_id,omitempty"`
	EpisodeRevision      uint64              `json:"episode_revision,omitempty"`
	ProjectSnapshotID    string              `json:"project_snapshot_id"`
	ControllerGeneration uint64              `json:"controller_generation"`
	TransitionID         string              `json:"transition_id"`
	BaseOID              string              `json:"base_oid"`
	CommitOID            string              `json:"commit_oid"`
	TreeOID              string              `json:"tree_oid"`
	SnapshotID           string              `json:"snapshot_id"`
	Message              string              `json:"message"`
	State                string              `json:"state"`
	DescendantTip        string              `json:"descendant_tip,omitempty"`
	DescendantEvidence   []EvidenceObjectRef `json:"descendant_evidence,omitempty"`
	EvidenceValid        bool                `json:"evidence_valid"`
	Evidence             []EvidenceObjectRef `json:"evidence"`
	GitArchive           EvidenceObjectRef   `json:"git_archive"`
	SealRef              EvidenceObjectRef   `json:"seal_ref"`
	Policy               PublicationPolicy   `json:"policy"`
	ObservedRemoteOID    string              `json:"observed_remote_oid,omitempty"`
	CreatedAt            time.Time           `json:"created_at"`
}

type CandidateAcceptanceInput struct {
	Message  string              `json:"message"`
	Policy   PublicationPolicy   `json:"policy"`
	Evidence []EvidenceObjectRef `json:"evidence"`
}

type PublicationInput struct {
	ExpectedGeneration uint64 `json:"expected_generation"`
	CandidateID        string `json:"candidate_id"`
}

type ExternalAdvancementInput struct {
	ExpectedGeneration uint64            `json:"expected_generation"`
	Policy             PublicationPolicy `json:"policy"`
}

type PublicationOperation struct {
	AdvancementRef *EvidenceObjectRef      `json:"advancement_ref,omitempty"`
	Before         *EvidenceObjectRef      `json:"before,omitempty"`
	After          *EvidenceObjectRef      `json:"after,omitempty"`
	Policy         PublicationPolicy       `json:"policy"`
	OldTip         string                  `json:"old_tip"`
	NewTip         string                  `json:"new_tip"`
	RemoteOID      string                  `json:"remote_oid"`
	LocalNew       string                  `json:"local_new,omitempty"`
	LocalOld       string                  `json:"local_old,omitempty"`
	SourceTrees    *ExecutionTrees         `json:"source_trees,omitempty"`
	Rebound        *ReboundSuspension      `json:"rebound,omitempty"`
	Project        *ProjectSnapshot        `json:"project,omitempty"`
	Episode        *BlockerEpisodeRevision `json:"episode,omitempty"`
}

type PublicationObservation struct {
	SchemaVersion int               `json:"schema_version"`
	TransitionID  string            `json:"transition_id"`
	CandidateID   string            `json:"candidate_id"`
	Remote        string            `json:"remote"`
	RemoteRef     string            `json:"remote_ref"`
	RemoteOID     string            `json:"remote_oid"`
	CandidateOID  string            `json:"candidate_oid"`
	GitArchive    EvidenceObjectRef `json:"git_archive"`
}

const (
	publicationAccept     = "ACCEPT_EXECUTION_ATTEMPT"
	publicationPromote    = "PROMOTE_UNOBSERVED_CANDIDATE"
	publicationPublish    = "PUBLISH_ACCEPTED_TASK"
	publicationRebind     = "REBIND_UNPUBLISHED_CANDIDATE"
	publicationAdopt      = "ADOPT_EXTERNAL_ADVANCEMENT"
	publicationReenter    = "REENTER_ACCEPTED_CANDIDATE"
	publicationRevalidate = "REVALIDATE_CANDIDATE"
	candidatePrepared     = "prepared"
	candidatePromoted     = "locally-promoted-unobserved"
	candidateObserved     = "remotely-observed"
)
